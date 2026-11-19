# Stable native releases

Release preparation is owned by the shared `../publish.sh` publisher, not a
project-specific release configuration. It recognizes this Go CLI, Swift helper,
Make payload layout and Formula automatically.

- Version comes from `VERSION.txt`; date is the compilation date.
- Release builds include Apple Silicon and Intel in a universal native payload.
  Both architectures install precompiled binaries. Intel and older supported
  macOS versions have not received additional runtime acceptance testing.
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

`make build-universal` cross-compiles both Go and Swift slices and joins them
before release signing. The shared publisher detects that target and verifies
that every executable has the same architecture set. A universal archive and
architecture-named download aliases contain the same signed payload; all are
included in `SHA256SUMS`. The generated Formula has no architecture restriction
for this payload. Homebrew bottles are tested on the build host; Intel uses the
precompiled universal archive when no Intel bottle is available, with no source
build. The checked-in Formula is replaced only after the new payload passes
notarization; previous archive checksums must never be reused for new binaries.
