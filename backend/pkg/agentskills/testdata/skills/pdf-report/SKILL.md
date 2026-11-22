---
name: pdf-report
description: Turn tabular query results into a paginated PDF report with a cover page, section headings and a totals table. Use when the user asks for a printable or emailable report rather than a chat answer.
license: MIT
compatibility:
  - claude-code
  - vibesdk
metadata:
  author: vibe-platform
  category: reporting
allowed-tools:
  - mcp/reports/render_pdf@1.2.0
  - mcp/reports/upload@1.0.0
  - render_pdf
---

# PDF Report

## When to use

Use this skill when the answer must survive outside the chat: a shared
document, a print-out, or an email attachment.

## Steps

1. Confirm the column mapping with the user before rendering.
2. Call `render_pdf` with the pinned column order; never let the model reorder
   columns silently.
3. Upload the artifact and return the link — a file that only exists in the
   run directory is not a deliverable.

## Output contract

- One page per 40 data rows, with a repeated header row.
- A totals table on the last page when the query returns numeric columns.
