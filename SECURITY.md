# Security Policy

## Supported Versions

klipbord is a small, actively developed project. Only the **latest release** receives
security updates. Please upgrade to the most recent tagged release before reporting
a vulnerability.

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| < latest| :x:                |

## Reporting a Vulnerability

**Do not open a public GitHub issue** to report a security vulnerability.

Instead, please use GitHub's **Private Vulnerability Reporting** feature:

1. Navigate to the [klipbord repository](https://github.com/jeeftor/klipbord).
2. Click the **Security** tab.
3. Select **Report a vulnerability** and submit a GitHub Security Advisory.

This keeps the report private to the maintainers and allows us to coordinate a fix
before public disclosure. Please include:

- A clear description of the issue and its impact.
- Steps to reproduce (proof of concept, if possible).
- Affected versions / commits.
- Any suggested remediation.

## Response Timeline

- **Acknowledgment:** within **72 hours** of the initial report.
- **Status updates:** at least every 7 days until the issue is resolved or closed.
- **Disclosure:** coordinated with the reporter once a fix is available, typically
  alongside a new release.

## Supply-Chain Security

Each release of klipbord publishes the following supply-chain artifacts:

- **Software Bill of Materials (SBOM)** in SPDX JSON format, generated with
  [Trivy](https://github.com/aquasecurity/trivy) and attached to the GitHub
  release (see the `SBOM` workflow).
- **Build provenance attestations** for the published OCI image on
  `ghcr.io/jeeftor/klipbord`, generated with
  [`actions/attest-build-provenance`](https://github.com/actions/attest-build-provenance)
  (see the `attest` job in the `Build and Push Docker Image` workflow).
- **SBOM attestations** for the published OCI image, generated with
  [`actions/attest-sbom`](https://github.com/actions/attest-sbom).

You can verify these attestations locally with
[`gh attestation verify`](https://cli.github.com/manual/gh_attestation_verify):

```bash
gh attestation verify ghcr.io/jeeftor/klipbord:<tag> \
  --owner jeeftor
```

Continuous vulnerability scanning is performed via the `Security` workflow, which
runs [Trivy](https://github.com/aquasecurity/trivy) and
[`govulncheck`](https://golang.org/x/vuln/cmd/govulncheck) on every push to
`main`, on pull requests, and on a weekly schedule.
