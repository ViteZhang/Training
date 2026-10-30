// 使用 Expo 分享模块接收 Android SEND；iOS 扩展采用标准保存流程，不从扩展强拉主 App。
const { withDangerousMod, withAndroidManifest, withMainActivity } = require('@expo/config-plugins');
const fs = require('node:fs');
const path = require('node:path');

module.exports = function withFileReceive(config) {
  config = withDangerousMod(config, ['ios', async (mod) => {
    const target = path.join(mod.modRequest.platformProjectRoot, 'expo-sharing-extension', 'ShareIntoViewController.swift');
    fs.copyFileSync(path.join(__dirname, 'ios', 'ShareIntoViewController.swift'), target);
    return mod;
  }]);
  // "其他应用打开" 常发送 ACTION_VIEW；Expo Sharing 本身只解析 SEND。
  config = withAndroidManifest(config, (mod) => {
    const application = mod.modResults.manifest.application[0];
    const activity = application.activity.find((item) => item.$['android:name'] === '.MainActivity');
    const types = ['application/pdf', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 'image/*'];
    const filters = activity['intent-filter'] ?? [];
    if (!filters.some((filter) => filter.$?.['android:label'] === '导入学习资料')) filters.push({
      $: { 'android:label': '导入学习资料' },
      action: [{ $: { 'android:name': 'android.intent.action.VIEW' } }],
      category: [{ $: { 'android:name': 'android.intent.category.DEFAULT' } }],
      data: types.flatMap((mimeType) => ['content', 'file'].map((scheme) => ({ $: { 'android:scheme': scheme, 'android:mimeType': mimeType } }))),
    });
    activity['intent-filter'] = filters;
    return mod;
  });
  // ACTION_VIEW 的 content URI 常没有扩展名；交给 Expo 的解析器读取 MIME 与显示名。
  config = withMainActivity(config, (mod) => {
    if (mod.modResults.language !== 'kt') throw new Error('文件接收需要 Kotlin MainActivity');
    let source = mod.modResults.contents;
    if (!source.includes('private fun normalizeFileImport')) {
      source = source.replace('    super.onCreate(null)', '    intent = normalizeFileImport(intent)\n    super.onCreate(null)');
      source = source.replace('class MainActivity : ReactActivity() {', `class MainActivity : ReactActivity() {
  private fun normalizeFileImport(source: android.content.Intent): android.content.Intent {
    val uri = source.data ?: return source
    val mime = source.type ?: contentResolver.getType(uri) ?: return source
    if (source.action != android.content.Intent.ACTION_VIEW || uri.scheme !in listOf("file", "content")) return source
    return android.content.Intent(source).apply {
      action = android.content.Intent.ACTION_SEND
      type = mime
      putExtra(android.content.Intent.EXTRA_STREAM, uri)
    }
  }
  override fun onNewIntent(intent: android.content.Intent) {
    val incoming = normalizeFileImport(intent)
    setIntent(incoming)
    super.onNewIntent(incoming)
  }
`);
      mod.modResults.contents = source;
    }
    return mod;
  });
  return config;
};
