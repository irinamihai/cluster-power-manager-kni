---
name: Bug report
about: Create a report to help us improve
title: '[Bug]:'
labels: ''
assignees: ''
type: Bug

---

**Describe the bug**

A clear and concise description of what the bug is.

**To Reproduce**

Steps to reproduce the behavior:
1. 
2. 
3. 
4. 

**Expected behavior**

A clear and concise description of what you expected to happen.

**Kubernetes version**
```
$ kubectl version
# paste output here
```

**Cluster Power Manager deployment information**

How did you deploy?
- [ ] kustomize (make install deploy)
- [ ] helm
- [ ] other (please describe)

If deployed with a pre-built image provide the image repository name and tag
```
repository: (e.g. ghcr.io)
tag: 
```
If deployed from an image you built, provide the git commit
```
$ git log -n 1 --oneline
# paste output here
```

What hardware are you using?
- Vendor (Intel, AMD, ARM):
- Architecture (x86, ARM):

**Additional context**

Add any other context about the problem here.
