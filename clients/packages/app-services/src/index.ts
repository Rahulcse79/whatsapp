// The shared application service layer used by BOTH clients.
//
// M1a added this package's manifest and the @wa/client-core ports it will be
// built on (KeyValueStore, DeviceCapabilities), ahead of moving the service
// layer here in M1b. The barrel is intentionally empty until that move lands:
// its job right now is to be a valid package, since package.json points `main`
// and `types` at this file and `tsconfig.json` includes this directory.
//
// Extraction target (Docs/MOBILE-PARITY.md): conversations, contacts, groups,
// channels, communities, stories, call control, collaboration, notifications
// and payments — with every platform difference arriving through a port, so
// this package keeps no DOM and no React-Native imports.

export {};
