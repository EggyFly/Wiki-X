---
title: Wiki-X
description: An open-source Go wiki framework with Git and PostgreSQL content modes
updated: 2026-10-20
translation_key: home
tags:
  - framework
  - wiki
---

This page demonstrates content localization. The English route is prefixed with `/en/`, and the page chrome uses the English message catalog automatically.

## What works today

- Git and PostgreSQL content modes sharing the same Markdown renderer and themes
- Local accounts, rights, page editing, pending revisions and review
- Edit conflict checks, unified diffs and audit logs
- GitHub PR creation from Git mode and one-way PR import in PostgreSQL mode
- Memory and Redis page caching, ETag/304, gzip and health checks
- The default and compact skins, plus external themes
- Chinese and English UI catalogs

## Content localization

The language switcher appears when the same base route exists under another locale prefix. In this demo only the home page has an English translation; use [/guide/](/guide/) for the Chinese guide.

> Content translation currently uses explicit locale-prefixed routes. A full translation workflow is still on the roadmap.
