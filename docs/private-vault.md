# Private Vault Contract

`Learning-Vault-Private` is a permanently private Obsidian vault with this
required structure:

```text
Learning-Vault-Private/
├── 00 Dashboard/
├── 01 Courses/
├── 02 Study/
├── 03 Draft Knowledge/
├── 04 Personal/
├── Templates/
├── .obsidian/
├── .studypilot/
├── README.md
└── .gitignore
```

The vault may contain transcripts, recordings, paid-course assets, rough notes,
questions, thoughts, reflections, assessments, progress records, knowledge
gaps, and candidates that might later inspire new public notes. This content is
private source material, not publication-ready content.

Private notes default to:

```yaml
---
visibility: private
---
```

The vault README must clearly warn that the repository may contain copyrighted
paid-course material and must never be made public. The `.studypilot` directory
is reserved for future StudyPilot state; no state format is defined yet.
