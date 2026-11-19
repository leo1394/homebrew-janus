# Stable native releases

Release preparation is owned by the shared `../publish.sh` publisher, not a
project-specific release configuration. It recognizes this Go CLI, Swift helper,
Make payload layout and Formula automatically.

- Version comes from `VERSION.txt`; date is the compilation date.
- Current precompiled distribution is Apple Silicon only. Intel and older
  supported macOS versions have not received additional acceptance testing.
- Both executables use Developer ID Application signing, hardened runtime and a
  secure timestamp. CLI identifier: `com.janus.ai.cli`; helper identifier:
  `com.janus.ai.cli.keychain`.
- The publisher preserves credentials in a local notarytool Keychain profile.
  Set `PUBLISH_NOTARY_PROFILE` to its name. Never commit passwords, certificate
  private keys or `signing.local.mk`.

From the parent Homebrew directory:

```sh
PUBLISH_NOTARY_PROFILE=janus-notary ./publish.sh --target homebrew-janus --prepare --bottle
# Review and commit the prepared source and generated Formula.
./publish.sh --target homebrew-janus --apply --bottle
```

Preparation tests a source snapshot, signs the native payload, submits a ZIP for
Apple notarization and requires an Accepted result. Submission IDs and signed
artifacts are retained in the publisher cache for recovery. Source changes create
an independent candidate; completed signatures are not silently reused for
changed inputs.

The publisher builds a real Homebrew bottle from the accepted payload, runs the
Formula test and checks that the CLI/helper bytes retain their signatures. No
local compiler is needed for installation: if no matching bottle is available on
a supported architecture, the Formula still installs the precompiled archive.
The installer and release `SHA256SUMS` refer to matching immutable assets.

Apply requires a clean committed preparation. It publishes an annotated version
tag and a draft GitHub Release, downloads and verifies the uploaded assets, then
makes the release public. Existing tags are never moved. A failed or incomplete
notarization cannot publish a stable Formula or release.

Before publishing, remove private host addresses and account names from source,
tests and diagnostic notes. Use reserved documentation addresses in fixtures.
