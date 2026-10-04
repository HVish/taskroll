# EPIC 0: Checkout
<!-- Generated from ../data/epic-0-checkout.jsonl. Change it with the tracker CLI, never by hand. -->

> Sizes: S is about a day, M two to three days, L a week. A blocker holds up the tasks that depend on it.

Everything between the cart and the order confirmation.

- [x] <a id="co-001"></a>**CO-001** - Cart page with quantity editing `M` · Quarter: Q3 · Done: 2026-08-24
- [x] <a id="co-002"></a>**CO-002** - Guest checkout `M` · Depends: CO-001 · Quarter: Q3 · Done: 2026-09-09
- [ ] <a id="co-003"></a>**CO-003** - Card payments through the payment provider · **[BLOCKER]** `L` · Depends: CO-002 · Quarter: Q3
- [ ] <a id="co-004"></a>**CO-004** - Order confirmation email `S` · Depends: CO-003 · Quarter: Q3
- [ ] <a id="co-005"></a>**CO-005** - Saved addresses for signed-in shoppers `M` · Depends: CO-002 · Quarter: Q4
- [ ] <a id="co-006"></a>**CO-006** - Refunds from the order page `L` · Depends: CO-003, a finance sign-off on partial refunds · Quarter: Q4
- [ ] <a id="co-007"></a>**CO-007** - Discount codes `M` · Quarter: Q4
- [x] <a id="co-008"></a>**CO-008** `bug` - Totals round wrongly for three-decimal currencies `S` · Quarter: Q3 · Done: 2026-09-24 · Urgent
