# Architecture

StudyPilot is designed around three repositories that remain operationally and
historically separate.

## StudyPilot

`studypilot` is the public Go application repository. It contains source code,
tests, documentation, releases, and synthetic fixtures. It must contain no real
transcripts, personal notes, paid-course assets, recordings, credentials, or
other private learning content.

## Learning-Vault-Private

`Learning-Vault-Private` is a permanently private Obsidian vault. It may contain
paid-course transcripts, recordings, course screenshots, raw notes, thoughts,
questions, assessments, knowledge gaps, private reflections, and draft
knowledge. It must never be converted to a public repository.

## IT-Knowledge-Portfolio

`IT-Knowledge-Portfolio` is a public, employer-facing Obsidian vault. It contains
original concept explanations, verified procedures, troubleshooting records,
personally performed lab reports, project summaries, and sanitized professional
retrospectives.

## System boundary

StudyPilot will eventually create and validate the private vault and public
portfolio as separate workspaces. They must have separate Git histories and
must never be treated as one repository.

Public notes are reviewed derivatives, not copies of private files. A private
source first becomes a private draft, is rewritten and verified, passes a
publication review, receives explicit human approval, and only then informs the
creation of a separate public artifact.

This document states the architecture contract. The current skeleton does not
yet implement or enforce it.
