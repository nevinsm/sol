# Execution Guidelines — Analysis

Follow these phases in order.

## 1. Understand

- Read the writ description carefully — identify the investigation scope.
- Clarify what output is expected: a report, a recommendation, structured data?
- Read any dependency outputs referenced in your assignment.

## 2. Investigate

- Explore thoroughly. Read code, logs, documentation — whatever the writ demands.
- Gather evidence. Note specific file paths, line numbers, and relevant snippets.
- Note what you looked at and what you found — also note what you didn't find.
- Cast a wide net before narrowing focus.

## 3. Document

- Write your findings to the writ output directory.
- Structure as:
  - **Summary** — one-paragraph answer to the writ's question
  - **Evidence** — specific findings with references (file:line, quotes, data)
  - **Recommendations** — actionable next steps based on your findings
- Save incrementally — partial results are better than none if your session dies.

## 4. Resolve

When your analysis is complete:
- `sol resolve`
