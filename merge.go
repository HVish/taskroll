package taskroll

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MergeResult is the outcome of a record-level three-way merge.
type MergeResult struct {
	Data []byte
	// Conflicts are the changes both sides made differently. Each was
	// resolved to ours so the file still decodes; the caller reports them and
	// fails the merge so a person looks.
	Conflicts []string
	// Renamed maps an ID both sides added for different work to the ID the
	// renumbered side's item was given.
	Renamed map[string]string
}

// MergeOptions carries what one file's three versions cannot tell Merge.
type MergeOptions struct {
	// Reserved IDs are issued elsewhere (other files, other branches); a
	// renumbered item goes past them.
	Reserved map[string]bool
	// Mainline is the file as the default branch has it, empty when unknown.
	// A colliding record found there unchanged is published: people have
	// seen its ID, so it keeps it and the other side is renumbered.
	Mainline []byte
	// Today dates the comments a conflict leaves on its record; empty means
	// the local date.
	Today string
}

// mergeAuthor signs the comments the merge driver leaves.
const mergeAuthor = "merge driver"

// Merge combines two edits of one JSONL file record by record. Git's line
// merge conflicts whenever both sides touch neighbouring lines, which is
// every time two branches append to one epic; keyed by ID, those are two
// independent additions.
//
//   - A record changed on one side takes that side.
//   - A record changed on both sides merges field by field: lists (labels,
//     depends, closes, notes, comments) take both sides' additions and
//     removals, the fields map merges per key, and anything else changed
//     differently on both sides is a conflict.
//   - Status and its stamps (done, closed_at, closed_by) merge as one unit,
//     since mixing them would describe a state neither side was in.
//   - Whatever a conflict resolves away is kept as a comment on the record:
//     the losing side's whole record, or the renumbering. The merge still
//     fails for a person to look, but one who runs `git add` without
//     reading loses nothing.
//   - An ID both sides added for different work is renumbered on one side,
//     past every reserved ID and every ID in the three versions, with that
//     side's references to it rewritten. Theirs is renumbered unless theirs
//     is the published record (see MergeOptions.Mainline): in `git merge
//     feature` and in a rebase theirs is the new work, but in a `git pull`
//     theirs is what is already on the default branch. References in other
//     files are not visible here, so the caller reports the renumbering.
//
// Records keep ours' order, and theirs' new records follow in their order.
func Merge(base, ours, theirs []byte, opt MergeOptions) (MergeResult, error) {
	var res MergeResult
	b, err := Decode("base", base)
	if err != nil {
		return res, err
	}
	o, err := Decode("ours", ours)
	if err != nil {
		return res, err
	}
	t, err := Decode("theirs", theirs)
	if err != nil {
		return res, err
	}
	baseItems, ourItems, theirItems := b.Records(), o.Records(), t.Records()
	bm, om, tm := byID(baseItems), byID(ourItems), byID(theirItems)

	var mainline map[string]Item
	if len(opt.Mainline) > 0 {
		if m, err := Decode("mainline", opt.Mainline); err == nil {
			mainline = byID(m.Records())
		}
	}
	taken := map[string]bool{}
	for id := range opt.Reserved {
		taken[id] = true
	}
	for _, m := range []map[string]Item{bm, om, tm} {
		for id := range m {
			taken[id] = true
		}
	}
	renameOurs, renameTheirs := map[string]string{}, map[string]string{}
	for _, it := range theirItems {
		oi, inOurs := om[it.ID]
		if _, inBase := bm[it.ID]; inBase || !inOurs || same(oi, it) || it.Type == TypeEpic {
			continue
		}
		next, err := nextFree(it.ID, taken)
		if err != nil {
			return res, err
		}
		taken[next] = true
		side, which := renameTheirs, "theirs"
		if pub, ok := mainline[it.ID]; ok && same(pub, it) && !same(pub, oi) {
			side, which = renameOurs, "ours"
		}
		side[it.ID] = next
		if res.Renamed == nil {
			res.Renamed = map[string]string{}
		}
		res.Renamed[it.ID] = next
		res.Conflicts = append(res.Conflicts, fmt.Sprintf("%s was added on both sides; %s is now %s. Check references to it in other files and in commit messages", it.ID, which, next))
	}
	slices.Sort(res.Conflicts)
	if len(renameOurs) > 0 {
		for i := range ourItems {
			ourItems[i] = renameRefs(ourItems[i], renameOurs)
		}
		om = byID(ourItems)
	}
	if len(renameTheirs) > 0 {
		for i := range theirItems {
			theirItems[i] = renameRefs(theirItems[i], renameTheirs)
		}
		tm = byID(theirItems)
	}

	today := opt.Today
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	note := func(it *Item, body string) {
		it.Comments = append(it.Comments, Comment{Date: today, Author: mergeAuthor, Body: body})
	}
	theirVersion := func(it Item) string {
		return "Their version of the record:\n\n```json\n" + string(encoded(it)) + "\n```"
	}
	conflict := func(format string, a ...any) { res.Conflicts = append(res.Conflicts, fmt.Sprintf(format, a...)) }
	var merged []Item
	for _, oi := range ourItems {
		bi, inBase := bm[oi.ID]
		ti, inTheirs := tm[oi.ID]
		switch {
		case !inTheirs && !inBase:
			merged = append(merged, oi)
		case !inTheirs:
			if same(oi, bi) {
				continue
			}
			conflict("%s: deleted on their side and changed on ours; kept ours", oi.ID)
			note(&oi, "Merge conflict: deleted on their side and changed on ours; kept ours.")
			merged = append(merged, oi)
		case !inBase:
			merged = append(merged, oi)
		default:
			it, cs := mergeItem(bi, oi, ti)
			for _, c := range cs {
				conflict("%s: %s", oi.ID, c)
			}
			if len(cs) > 0 {
				note(&it, "Merge conflict: "+strings.Join(cs, "; ")+". "+theirVersion(ti))
			}
			merged = append(merged, it)
		}
	}
	for _, ti := range theirItems {
		if _, inOurs := om[ti.ID]; inOurs {
			continue
		}
		bi, inBase := bm[ti.ID]
		if !inBase {
			merged = append(merged, ti)
			continue
		}
		if same(ti, bi) {
			continue
		}
		conflict("%s: deleted on our side and changed on theirs; kept theirs", ti.ID)
		note(&ti, "Merge conflict: deleted on our side and changed on theirs; kept theirs.")
		merged = append(merged, ti)
	}
	for i := range merged {
		for old, next := range res.Renamed {
			if merged[i].ID == next {
				note(&merged[i], "Renumbered from "+old+" at a merge: both sides had added "+old+" for different work.")
			}
		}
	}

	out := &File{}
	for i, it := range merged {
		if it.Type == TypeEpic {
			out.Epic = it
			out.Items = slices.Concat(merged[:i], merged[i+1:])
			break
		}
	}
	if !out.IsEpic() {
		if o.IsEpic() || t.IsEpic() {
			return res, fmt.Errorf("the merged file has no epic record")
		}
		out.Items = merged
	}
	if out.Epic.Archived != "" && len(out.Items) > 0 {
		conflict("%s was archived on one side and still has items from the other (%s first); move them to an open epic", out.Epic.ID, out.Items[0].ID)
	}
	res.Data, err = Encode(out)
	return res, err
}

func byID(items []Item) map[string]Item {
	m := make(map[string]Item, len(items))
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

func encoded(it Item) []byte {
	raw, _ := json.Marshal(canonical(it))
	return raw
}

func same(a, b Item) bool { return bytes.Equal(encoded(a), encoded(b)) }

// closeKeys change together: status decides whether the other three may be
// set at all.
var closeKeys = []string{"status", "done", "closed_at", "closed_by"}

var listKeys = []string{"depends", "closes", "notes", "labels", "comments"}

func mergeItem(b, o, t Item) (Item, []string) {
	if same(o, b) {
		return t, nil
	}
	if same(t, b) || same(o, t) {
		return o, nil
	}
	bm, om, tm := fieldsOf(b), fieldsOf(o), fieldsOf(t)
	out := map[string]json.RawMessage{}
	var conflicts []string

	if pick, ok := merge3(group(bm, closeKeys), group(om, closeKeys), group(tm, closeKeys)); ok && pick == 't' {
		copyKeys(out, tm, closeKeys)
	} else {
		copyKeys(out, om, closeKeys)
		if !ok {
			conflicts = append(conflicts, fmt.Sprintf("status or its stamps changed on both sides (ours %s %s, theirs %s %s); kept ours", o.Status, o.Done, t.Status, t.Done))
		}
	}
	keys := map[string]bool{}
	for _, m := range []map[string]json.RawMessage{bm, om, tm} {
		for k := range m {
			keys[k] = true
		}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	slices.Sort(sorted)
	for _, k := range sorted {
		switch {
		case slices.Contains(closeKeys, k):
		case slices.Contains(listKeys, k):
			setRaw(out, k, mergeList(bm[k], om[k], tm[k]))
		case k == "fields":
			v, cs := mergeFields(bm[k], om[k], tm[k])
			setRaw(out, k, v)
			conflicts = append(conflicts, cs...)
		default:
			pick, ok := merge3(bm[k], om[k], tm[k])
			if !ok {
				conflicts = append(conflicts, fmt.Sprintf("%s changed on both sides; kept ours", k))
			}
			if pick == 't' {
				setRaw(out, k, tm[k])
			} else {
				setRaw(out, k, om[k])
			}
		}
	}
	raw, _ := json.Marshal(out)
	var it Item
	if err := decodeStrict(raw, &it); err != nil {
		return o, append(conflicts, "merged record did not decode ("+err.Error()+"); kept ours")
	}
	if err := it.Validate(); err != nil {
		return o, append(conflicts, "merged record is invalid ("+err.Error()+"); kept ours")
	}
	return it, conflicts
}

func fieldsOf(it Item) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(encoded(it), &m)
	return m
}

func group(m map[string]json.RawMessage, keys []string) json.RawMessage {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = string(m[k])
	}
	return json.RawMessage(strings.Join(parts, "\x00"))
}

func copyKeys(dst, src map[string]json.RawMessage, keys []string) {
	for _, k := range keys {
		setRaw(dst, k, src[k])
	}
}

func setRaw(m map[string]json.RawMessage, k string, v json.RawMessage) {
	if v == nil {
		return
	}
	m[k] = v
}

// merge3 picks the side to keep: 'o' when theirs made no change or both
// made the same one, 't' when only theirs changed, and ok=false when both
// changed differently.
func merge3(b, o, t json.RawMessage) (byte, bool) {
	switch {
	case bytes.Equal(o, b):
		return 't', true
	case bytes.Equal(t, b), bytes.Equal(o, t):
		return 'o', true
	default:
		return 'o', false
	}
}

// mergeList keeps ours' order, drops what theirs removed, and appends what
// theirs added. Entries compare by their encoded value.
func mergeList(b, o, t json.RawMessage) json.RawMessage {
	var bl, ol, tl []json.RawMessage
	_ = json.Unmarshal(b, &bl)
	_ = json.Unmarshal(o, &ol)
	_ = json.Unmarshal(t, &tl)
	has := func(l []json.RawMessage, v json.RawMessage) bool {
		return slices.ContainsFunc(l, func(x json.RawMessage) bool { return bytes.Equal(x, v) })
	}
	var out []json.RawMessage
	for _, v := range ol {
		if has(bl, v) && !has(tl, v) {
			continue
		}
		out = append(out, v)
	}
	for _, v := range tl {
		if !has(bl, v) && !has(out, v) {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	raw, _ := json.Marshal(out)
	return raw
}

func mergeFields(b, o, t json.RawMessage) (json.RawMessage, []string) {
	var bm, om, tm map[string]json.RawMessage
	_ = json.Unmarshal(b, &bm)
	_ = json.Unmarshal(o, &om)
	_ = json.Unmarshal(t, &tm)
	keys := map[string]bool{}
	for _, m := range []map[string]json.RawMessage{bm, om, tm} {
		for k := range m {
			keys[k] = true
		}
	}
	out := map[string]json.RawMessage{}
	var conflicts []string
	for k := range keys {
		pick, ok := merge3(bm[k], om[k], tm[k])
		if !ok {
			conflicts = append(conflicts, fmt.Sprintf("field %s changed on both sides; kept ours", k))
		}
		if pick == 't' {
			setRaw(out, k, tm[k])
		} else {
			setRaw(out, k, om[k])
		}
	}
	slices.Sort(conflicts)
	if len(out) == 0 {
		return nil, conflicts
	}
	raw, _ := json.Marshal(out)
	return raw, conflicts
}

var seriesNumber = regexp.MustCompile(`^([A-Za-z]+)-([0-9]{3})(\.[0-9]+)?$`)

// nextFree returns one past the highest number taken in id's series, or for
// a slug the first of slug-2, slug-3, ... not taken.
func nextFree(id string, taken map[string]bool) (string, error) {
	m := seriesNumber.FindStringSubmatch(id)
	if m == nil {
		if !slugID.MatchString(id) {
			return "", fmt.Errorf("%s was added on both sides and is neither numbered nor a slug", id)
		}
		for n := 2; ; n++ {
			if next := fmt.Sprintf("%s-%d", id, n); !taken[next] {
				return next, nil
			}
		}
	}
	highest := 0
	for t := range taken {
		if tm := seriesNumber.FindStringSubmatch(t); tm != nil && strings.EqualFold(tm[1], m[1]) {
			if n, _ := strconv.Atoi(tm[2]); n > highest {
				highest = n
			}
		}
	}
	return fmt.Sprintf("%s-%03d", m[1], highest+1), nil
}

func renameRefs(it Item, renamed map[string]string) Item {
	if next, ok := renamed[it.ID]; ok {
		it.ID = next
	}
	res := make(map[string]*regexp.Regexp, len(renamed))
	for old := range renamed {
		res[old] = regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`)
	}
	for _, refs := range [][]string{it.Depends, it.Closes, it.Notes} {
		for i := range refs {
			for old, next := range renamed {
				refs[i] = res[old].ReplaceAllString(refs[i], next)
			}
		}
	}
	return it
}
