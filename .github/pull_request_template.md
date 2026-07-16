## What

<!-- One paragraph: the change and why. -->

**Task:** T<!-- e.g. 0.07 --> — <!-- task name from Docs/12-planning/task-breakdown.md -->
**Docs followed:** <!-- links into Docs/ that specify this change -->

## Checklist (delete lines that genuinely don't apply)

- [ ] Tests assert the behavior described in the linked docs (not just coverage)
- [ ] Error/retry paths handled per Docs/02-architecture/design-patterns-error-handling.md
- [ ] New endpoint/frame/subject ⇒ the matching Docs/04-api/ file is updated (CI-gated)
- [ ] Schema change ⇒ expand–contract respected; Docs/03-database/ updated (CI-gated)
- [ ] Query touches `message_inbox` ⇒ `EXPLAIN (ANALYZE, BUFFERS)` output attached below
- [ ] New attack surface ⇒ rows added to Docs/06-security/threat-model-abuse.md
- [ ] New failure mode ⇒ metric/log/alert added (Docs/09-observability/)
- [ ] No plaintext-content path introduced server-side (E2EE boundary — e2ee-design.md §8)

## Test evidence

<!-- CI run link is enough for unit/integration; paste output for anything manual. -->

## EXPLAIN (hot-path SQL only)

<!-- paste plan or delete section -->
