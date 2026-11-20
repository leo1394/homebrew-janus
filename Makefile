VERSION ?= $(strip $(shell cat VERSION.txt))
RELEASE_DATE ?= $(shell date +%Y-%m-%d)
GO ?= go
SWIFTC ?= swiftc
-include signing.local.mk
CODE_SIGN_IDENTITY ?= -
SWIFT_TARGET ?= $(shell uname -m)-apple-macosx14.0

.PHONY: build build-universal test test-native clean package
build: build/libexec/janus-keychain build/libexec/janus-keychain-migrate
	mkdir -p build/bin build/libexec build/share/janus
	$(GO) build -trimpath -ldflags '-X github.com/leo1394/homebrew-janus/internal/metadata.Version=$(VERSION) -X github.com/leo1394/homebrew-janus/internal/metadata.Date=$(RELEASE_DATE)' -o build/bin/.janus-new ./cmd/janus
	mv -f build/bin/.janus-new build/bin/janus
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.confidential.cli build/bin/janus
	cp install.sh build/share/janus/install.sh
	cd build && shasum -a 256 bin/janus libexec/janus-keychain libexec/janus-keychain-migrate share/janus/install.sh > SHA256SUMS

build/libexec/janus-keychain: native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift Makefile $(wildcard signing.local.mk)
	mkdir -p build/libexec
	$(SWIFTC) -O -target $(SWIFT_TARGET) -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/libexec/.janus-keychain-new -framework AppKit -framework Security -framework LocalAuthentication
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.confidential.cli.keychain build/libexec/.janus-keychain-new
	codesign --verify --strict build/libexec/.janus-keychain-new
	mv -f build/libexec/.janus-keychain-new build/libexec/janus-keychain

build/libexec/janus-keychain-migrate: native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift Makefile $(wildcard signing.local.mk)
	mkdir -p build/libexec
	$(SWIFTC) -O -D JANUS_IDENTITY_MIGRATOR -target $(SWIFT_TARGET) -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/libexec/.janus-migrate-new -framework AppKit -framework Security -framework LocalAuthentication
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.cli.keychain build/libexec/.janus-migrate-new
	codesign --verify --strict build/libexec/.janus-migrate-new
	mv -f build/libexec/.janus-migrate-new build/libexec/janus-keychain-migrate

test:
	$(GO) test ./...

test-native:
	./tests/native/test-acl-relocation.sh "$(CODE_SIGN_IDENTITY)"
	./tests/native/test-migration-caller.sh "$(CODE_SIGN_IDENTITY)"

package: build-universal
	./scripts/package.sh $(VERSION)

clean:
	rm -rf build dist

# Build both slices without running Intel binaries or requiring an Intel host.
build-universal:
	mkdir -p build/bin build/libexec build/share/janus build/slices
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags '-X github.com/leo1394/homebrew-janus/internal/metadata.Version=$(VERSION) -X github.com/leo1394/homebrew-janus/internal/metadata.Date=$(RELEASE_DATE)' -o build/slices/janus-arm64 ./cmd/janus
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags '-X github.com/leo1394/homebrew-janus/internal/metadata.Version=$(VERSION) -X github.com/leo1394/homebrew-janus/internal/metadata.Date=$(RELEASE_DATE)' -o build/slices/janus-x86_64 ./cmd/janus
	$(SWIFTC) -O -target arm64-apple-macosx14.0 -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/slices/helper-arm64 -framework AppKit -framework Security -framework LocalAuthentication
	$(SWIFTC) -O -target x86_64-apple-macosx14.0 -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/slices/helper-x86_64 -framework AppKit -framework Security -framework LocalAuthentication
	$(SWIFTC) -O -D JANUS_IDENTITY_MIGRATOR -target arm64-apple-macosx14.0 -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/slices/migrate-arm64 -framework AppKit -framework Security -framework LocalAuthentication
	$(SWIFTC) -O -D JANUS_IDENTITY_MIGRATOR -target x86_64-apple-macosx14.0 -module-cache-path $(CURDIR)/build/module-cache native/KeychainPolicy.swift native/IdentityMigration.swift native/main.swift -o build/slices/migrate-x86_64 -framework AppKit -framework Security -framework LocalAuthentication
	lipo -create build/slices/janus-arm64 build/slices/janus-x86_64 -output build/bin/janus
	lipo -create build/slices/migrate-arm64 build/slices/migrate-x86_64 -output build/libexec/janus-keychain-migrate
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.cli.keychain build/libexec/janus-keychain-migrate
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.confidential.cli build/bin/janus
	lipo -create build/slices/helper-arm64 build/slices/helper-x86_64 -output build/libexec/janus-keychain
	codesign --force --sign "$(CODE_SIGN_IDENTITY)" --options runtime --identifier com.janus.ai.confidential.cli.keychain build/libexec/janus-keychain
	codesign --verify --strict --all-architectures build/libexec/janus-keychain
	codesign --verify --strict --all-architectures build/libexec/janus-keychain-migrate
	codesign --verify --strict --all-architectures build/bin/janus
	cp install.sh build/share/janus/install.sh
	cd build && shasum -a 256 bin/janus libexec/janus-keychain libexec/janus-keychain-migrate share/janus/install.sh > SHA256SUMS
