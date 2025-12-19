---
description: Bump tekton jobs to a new OpenShift release
---

## Name

bump-tekton-jobs

## Synopsis

```
/bump-tekton-jobs [release]
```

## Description

Every time a new OpenShift release gets onboarded the following steps need to be carried:
- OCP_VERSION corresponds to the provided release argument
- PREV_OCP_VERSION = OCP_VERSION - 1
- Reading files with "git show" under "git show release-<PREV_OCP_VERSION>:.tekton" to see how 4.NN and 4-NN strings are used.
- Updating all files under the current .tekton directory and setting the ocp version to OCP_VERSION at the same locations as in the read files. Please do not use sed for the replacement as it might replace undesirable strings. Interpret all the yaml files before editing.
- Renaming all files under the current .tekton directory to correspond to OCP_VERSION

  ## Arguments

  - **$1** (release): Required. The OCP version the Tekton jobs are getting bumped to.
