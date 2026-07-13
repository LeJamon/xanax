# Security policy

Please report suspected vulnerabilities privately through GitHub's security
advisory interface for this repository. Do not open a public issue with exploit
details.

Supported releases receive fixes on the latest tagged minor line. Builds use a
fully patched Go toolchain, and CI scans reachable dependencies with
`govulncheck`.
