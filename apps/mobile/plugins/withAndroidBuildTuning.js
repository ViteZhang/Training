const { withGradleProperties, withProjectBuildGradle } = require('expo/config-plugins');

function setProperty(properties, key, value) {
  const property = properties.find((item) => item.type === 'property' && item.key === key);
  if (property) {
    property.value = value;
  } else {
    properties.push({ type: 'property', key, value });
  }
}

module.exports = function withAndroidBuildTuning(config) {
  config = withGradleProperties(config, (config) => {
    const properties = config.modResults;
    setProperty(properties, 'org.gradle.jvmargs', '-Xmx3072m -XX:MaxMetaspaceSize=1536m -Dfile.encoding=UTF-8');
    setProperty(properties, 'org.gradle.parallel', 'false');
    setProperty(properties, 'org.gradle.workers.max', '3');
    setProperty(properties, 'reactNativeArchitectures', 'arm64-v8a,armeabi-v7a');
    return config;
  });

  return withProjectBuildGradle(config, (config) => {
    const contents = config.modResults.contents;
    if (!contents.includes('maven.aliyun.com/repository/google')) {
      config.modResults.contents = contents.replace(
        /^([ \t]*)google\(\)/gm,
        (_, indent) => `${indent}maven { url 'https://maven.aliyun.com/repository/google' }\n${indent}google()`
      );
    }
    return config;
  });
};
