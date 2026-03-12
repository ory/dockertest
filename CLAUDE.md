- Always pass `context.Context` from caller (never use `context.Background` or
  `TODO`).
- Follow idiomatic Go conventions
  ([Effective Go](https://go.dev/doc/effective_go)).
- Prefer simple, clean architectures versus overengineering.
- Think about how you can remove, simplify, and reduce code (not tests!). LLMs
  and AI agents have a tendency to generate useless helpers that already exist
  in the stdlib or other libraries. Instead, I want you to delete and simplify
  code (do not delete test cases though) and not generate
  useless/duplicated/existing code. Generating useless code or duplicating
  already existing functionality results in contract termination.
- Implementation notes and plans and documents generated while developing belong
  in `./docs/notes/`. Ensure that notes have a frontmatter with `date`, and
  `reason` fields. Specifically, the current date is important and the reason
  why the note was created.
- When tests fail, do not remove or disable them. A failing test - especially if
  it already exists on the main branch - indicates a fault in the business
  logic. Removing or disabling that test leads to immediate contract
  termination.
- Determinism is key, especially in tests and conformity checks. Avoid any
  sources of non-determinism such as random number generation, time-based
  functions, or reliance on external systems that may introduce variability.
  Non-deterministic behavior can lead to flaky tests and unpredictable
  application behavior, which is unacceptable. It is also unacceptable to have
  thresholds in tests (e.g. 99% match X) - tests must be deterministic and
  exact.
- Do not use equality for error comparison. Always use errors.Is() or
  errors.As() for error comparisons.
- In tests don't use `defer` for cleanup but `t.Cleanup`.
- DO NOT use `github.com/docker/docker` but only `github.com/moby/moby/client`
