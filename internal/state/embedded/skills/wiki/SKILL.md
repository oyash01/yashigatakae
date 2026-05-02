---
name: wiki
description: Read the per-repo Karpathy LLM Wiki at ~/.yashigatakae/state/codebase-wiki/<repo-basename>/ before answering any question about the repo. Saves Claude from re-greping the codebase on every session start.
---

# /wiki — read before answering

When this skill is invoked OR when the user is working in a repo that has a wiki built, read the wiki BEFORE answering anything substantial about that repo.

## Where the wiki lives

Path: `~/.yashigatakae/state/codebase-wiki/<basename>/` where `<basename>` is the last path segment of the cwd.

If `~/.yashigatakae/state/codebase-wiki/<basename>/index.md` does not exist, run:

```
yashigatakae graphify "$(pwd)" --pro
```

This creates the wiki under that directory in 1–2 seconds.

## How to read it

1. **Read `index.md` first.** It contains the infobox (HEAD sha, file count, primary lang, module count, public symbol count, dep count, ADR count) and a navigation TOC.
2. **For architectural questions**, read `architecture.md` next (component map + per-module table).
3. **For "what does X do / who calls Y"** questions, read `symbols/<X>.md` — every public symbol gets a page with signature, doc, and "referenced by" backlinks.
4. **For "why was decision Z made"** questions, read `DECISIONS.md` — auto-extracted from substantive git commits.
5. **For "what's in module M"**, read `modules/<M>.md` — infobox + grouped symbol list.
6. **For domain term lookups**, check `GLOSSARY.md` — proper nouns appearing >3x in docs.
7. **For broken refs** (something the wiki mentions but doesn't define), check `STUB-PAGES.md` — those are the gaps to flag.

## Wikilink syntax

The wiki uses `[[Foo]]` markdown wikilinks. They render as relative markdown links. If you see `STUB-PAGES.md#foo` linked, the page is a stub — the concept exists but isn't documented yet.

## Refresh

If the wiki feels stale, re-run `yashigatakae graphify <repo> --pro` to regenerate. Incremental refresh and CI integration land in v0.13.1.

## Why this exists

Without the wiki, every Claude session in a non-trivial repo spends ~10K tokens grepping and reading source to build a mental map. With the wiki, those 10K tokens are pre-computed once and read from a few markdown files. Saves time, saves tokens, keeps Claude grounded in the same shared model the user has.
