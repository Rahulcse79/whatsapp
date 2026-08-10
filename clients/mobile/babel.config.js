// babel-preset-expo covers expo-router (no separate plugin needed on SDK 50+).
module.exports = function (api) {
  api.cache(true);
  return { presets: ["babel-preset-expo"] };
};
