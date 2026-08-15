// Expo Metro config tuned for the pnpm monorepo: watch the workspace root so
// edits in clients/packages/* hot-reload, and let Metro resolve both the app's
// own node_modules and the hoisted workspace root.
const { getDefaultConfig } = require("expo/metro-config");
const path = require("path");

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, "..");

const config = getDefaultConfig(projectRoot);
config.watchFolders = [workspaceRoot];
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, "node_modules"),
  path.resolve(workspaceRoot, "node_modules"),
];
config.resolver.disableHierarchicalLookup = true;

// Honor package.json "exports" subpaths — @wa/proto-types' generated code imports
// "@bufbuild/protobuf/codegenv2", an exports-only subpath that Metro can't
// resolve otherwise (Expo SDK 52's Metro leaves this opt-in; default in later
// SDKs). Without it the release JS bundle fails "could not be found".
config.resolver.unstable_enablePackageExports = true;

module.exports = config;
