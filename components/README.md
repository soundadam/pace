# Components and license boundaries

This directory contains separately built helper source. It is not covered by
the repository-root MIT license unless a component explicitly says so.

| Directory | Purpose | License |
| --- | --- | --- |
| `librespeed-cli` | Pinned campus speed-test helper | LGPL-3.0-only |

pace and other consumers execute the helper as a separate process. They do
not import or statically link its Go packages.
