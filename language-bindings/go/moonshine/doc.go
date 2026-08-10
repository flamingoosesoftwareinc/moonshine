// Package moonshine provides safe, higher-level access to the Moonshine voice
// APIs.
//
// The raw package contains mechanically generated bindings to the C API.
// Applications should normally use this package: it exposes Go-owned values,
// maps native failures to sentinel errors, and gives every native handle an
// idempotent Close method. Call Close explicitly; finalizers are only a
// fallback for abandoned resources.
//
// Model assets are never downloaded implicitly. Dependency and catalog APIs
// return native manifests which Downloader can verify and install when an
// application chooses to do so. Model-backed integration suites are opt-in.
//
// The higher-level packages remain injectable at their slow and platform-
// specific boundaries. MicTranscriber accepts an AudioCapture implementation,
// and AgentFlow composes narrow input, embedding, speech, and playback
// interfaces, allowing deterministic tests without models or microphones.
package moonshine
