---
title: "Tests present"
description: "Scans the project for common test file/dir conventions (*_test.go, test_*.py, *.test.{ts,js}, *.spec.{ts,js}, *_test.rs, *Test.java, *_spec.rb…"
---

# Tests present

Detects whether the project has any tests at all. It says nothing about coverage or quality — only that a test suite exists to run.

<GateMeta id="tests_present" severity="warning" tags="quality" scope="Whole project tree" />

## What it checks

Scans the project for common test file/dir conventions (*_test.go, test_*.py, *.test.{ts,js}, *.spec.{ts,js}, *_test.rs, *Test.java, *_spec.rb, conftest.py, tests/ directories).

Recognised conventions across languages: `*_test.go`, `test_*.py` /
`*_test.py`, `*.test.{ts,js,mjs,cjs}`, `*.spec.{ts,js,mjs,cjs}`, `*_test.rs`,
`*Test.{java,kt}`, `*Spec.kt`, `*_spec.rb`, `conftest.py`, plus `test/`,
`tests/`, `__tests__/` and `spec/` directories.

One match anywhere in the tree satisfies the gate.

## What a finding says

```text
No test files detected anywhere under the project (looked for *_test.go, test_*.py / *_test.py, *.test.{ts,js,mjs,cjs}, *.spec.{ts,js,mjs,cjs}, *_test.rs, …).
```

## Turning it off

Silence the gate for the whole project in `.l0git.json`:

```json
{
  "ignore": ["tests_present"]
}
```

Or keep it running at a lower severity:

```json
{
  "severity": { "tests_present": "info" }
}
```

## See also

- [CI workflow present](/gates/ci-workflow-present)
