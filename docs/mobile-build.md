# Android / iOS 本地开发与打包

项目位置：`/Users/a1/Documents/workspace/myself/peetraining-web`，移动端目录为 `apps/mobile`，管理后台 `apps/admin` 不参与手机 App 打包。项目使用 Expo + React Native，包含 MMKV 等原生模块，需要 development build，不能用 Expo Go。

## 1. 官方下载

| 软件/账号 | 官方地址 | 用途 |
| --- | --- | --- |
| Android Studio | https://developer.android.com/studio | Android SDK、模拟器、安卓原生编译 |
| Android Studio 安装说明 | https://developer.android.com/studio/install | 安装与系统要求 |
| Xcode（Mac App Store） | https://apps.apple.com/cn/app/xcode/id497799835 | iOS SDK、模拟器、签名和归档 |
| Xcode 历史版本 | https://developer.apple.com/download/all/ | 安装适合当前 macOS 的版本，需登录 Apple 账号 |
| Apple 开发者计划 | https://developer.apple.com/programs/ | TestFlight、App Store 分发 |
| App Store Connect | https://appstoreconnect.apple.com/ | 创建 App、管理 TestFlight 和上架 |
| Expo 账号 | https://expo.dev/signup | EAS 项目与构建凭据管理 |
| EAS 本地构建说明 | https://docs.expo.dev/build-reference/local-builds/ | `eas build --local` 的环境要求 |

当前 Mac 为 Apple Silicon，Android Studio 选择 **Mac with Apple chip / ARM** 安装包，模拟器选择 ARM64 系统镜像。Xcode 安装完整应用，只有 Command Line Tools 不足以编译 iOS。版本选择要同时满足当前 macOS 和项目 Expo/React Native 的要求；正式提交时还需满足 Apple 当前的 Xcode/SDK 要求。

## 2. 项目准备

需要项目支持的 Node.js LTS（package.json 要求 >=22）以及 pnpm 10.28.0；优先使用该 Expo SDK 支持的 Node LTS，不以 Node 最新非 LTS 作为构建基线。

在 Mac 终端执行：

```bash
cd /Users/a1/Documents/workspace/myself/peetraining-web
node --version
pnpm --version
pnpm install --frozen-lockfile

cd apps/mobile
pnpm exec expo install --check
npx expo-doctor@latest
```

依赖检查有错误时先根据报告排查，再生成原生工程。不要另外安装全局旧版 `expo-cli`。

配置入口为 `apps/mobile/app.config.ts`，构建配置为 `apps/mobile/eas.json`。API 地址已统一为 `https://training.dreamerlab.cn/api/v1`。`APP_ID` 默认仍是 `cn.dreamelab.training`：包标识符与域名是两种配置，不随 DNS 改动而自动改名。

| APP_VARIANT | 最终包标识符 | 用途 |
| --- | --- | --- |
| development | cn.dreamelab.training.development | 调试版 |
| preview | cn.dreamelab.training.preview | 内测版 |
| production | cn.dreamelab.training | 正式版 |

先确认后端正常：

```bash
curl -i https://training.dreamerlab.cn/api/v1/health
```

健康检查通过只代表基础后端可用；若短信、OSS 等仍为 disabled，App 相关业务仍会失败，与手机打包是否成功无关。

## 3. Android Studio 首次配置

1. 安装后打开 Android Studio，完成 Setup Wizard，安装 Android SDK。
2. 在 Settings → Languages & Frameworks → Android SDK（或欢迎页 More Actions → SDK Manager）安装 SDK Platform-Tools、Build-Tools、Command-line Tools (latest)。SDK Platform 安装后续 Expo 生成工程所要求的版本，若缺少平台组件按 Gradle 提示补装。
3. 打开 Device Manager → Create Device，选择手机型号和 ARM64 系统镜像，创建并启动模拟器。使用安卓真机时，开启开发者选项与 USB 调试，连接 Mac 后允许 USB 调试授权。
4. SDK 默认在 `~/Library/Android/sdk`，以 SDK Manager 显示的位置为准。把下面内容加入 `~/.zshrc`，只添加一次：

```bash
export ANDROID_HOME="$HOME/Library/Android/sdk"
export PATH="$PATH:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$ANDROID_HOME/cmdline-tools/latest/bin"
# 标准安装位置下的 Android Studio 自带 JDK；若应用安装在别处，使用实际路径。
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
export PATH="$JAVA_HOME/bin:$PATH"
```

重新打开终端，或运行 `source ~/.zshrc`，检查：

```bash
java -version
adb version
adb devices
```

命令行 Gradle 使用 `JAVA_HOME`；Android Studio 的 Gradle JDK 在 Settings → Build, Execution, Deployment → Build Tools → Gradle 配置。两处使用与项目 Gradle/AGP 兼容的 JDK，若报 Java 版本不兼容按实际错误调整，避免 IDE 与终端用不同 JDK。

### 安卓调试运行

先启动模拟器，或连接已授权的真机，再执行：

```bash
cd /Users/a1/Documents/workspace/myself/peetraining-web/apps/mobile
APP_VARIANT=development API_BASE_URL=https://training.dreamerlab.cn/api/v1 pnpm exec expo run:android
```

首次会生成 `apps/mobile/android`，编译并安装开发包，启动 Metro。真机可在末尾加 `--device` 选择设备。之后仅修改 JS/TS 时，通常只需启动 `pnpm start`；增加原生模块或改原生配置后需要重新编译。

需要在 Android Studio 查看原生工程时，打开生成的 `apps/mobile/android`，不要把 monorepo 根目录当作安卓工程。开发包用于连接 Metro，发给别人独立安装时使用下面的 APK 构建。

## 4. Xcode 首次配置与调试

1. 把 Xcode 安装到 `/Applications/Xcode.app`，打开一次，接受许可并完成组件安装。
2. 在 Xcode Settings → Platforms / Components 安装 iOS Simulator runtime；具体入口随版本变化。
3. 在 Xcode Settings → Accounts 登录 Apple 账号。模拟器运行不需要付费开发者会员；真机调试需要签名，个人免费团队有使用限制，TestFlight/App Store 需要 Apple Developer Program。
4. 在终端选择完整 Xcode 并完成初始化：

```bash
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -runFirstLaunch
xcode-select -p
xcodebuild -version
```

`xcode-select -p` 应显示 Xcode 的 Developer 目录，而不是 `/Library/Developer/CommandLineTools`。

5. 安装 CocoaPods（已有则不重复安装）：

```bash
brew install cocoapods
pod --version
```

运行 iOS 模拟器开发版：

```bash
cd /Users/a1/Documents/workspace/myself/peetraining-web/apps/mobile
APP_VARIANT=development API_BASE_URL=https://training.dreamerlab.cn/api/v1 pnpm exec expo run:ios
```

首次会生成 `apps/mobile/ios` 并安装 Pods。真机调试：iPhone 用数据线连接 Mac，按提示信任设备并开启 Developer Mode，然后使用：

```bash
APP_VARIANT=development API_BASE_URL=https://training.dreamerlab.cn/api/v1 pnpm exec expo run:ios --device
```

若签名报错，用 Xcode 打开 `apps/mobile/ios` 下生成的 `.xcworkspace`，在 App Target → Signing & Capabilities 选正确 Team，并开启 Automatically manage signing。安装 Pods 后打开 workspace，不是 `.xcodeproj`。开发版调试仍需 Metro；它不是分发给测试用户的独立 IPA。

## 5. 本地构建可分发 APK / AAB / IPA

推荐先用现有 EAS profiles，本机执行编译，凭据由 EAS 管理。`--local` 表示编译发生在 Mac；依赖下载、Expo 登录、证书管理仍需要网络。EAS 本地构建仍依赖本机 Android SDK/JDK 或 Xcode/CocoaPods。它与完全离线构建不同。

首次配置，在 `apps/mobile` 执行：

```bash
npx eas-cli@latest login
npx eas-cli@latest init
```

项目使用动态 `app.config.ts`。若 EAS 提示无法自动写入 projectId，按命令输出把真实 ID 加到 `extra` 下的 `eas: { projectId: '实际项目 UUID' }`，保留已有 `apiBaseUrl`、`variant`。根据 EAS 提示补充对应 Expo owner；不要把 Apple/OSS/后端私钥写进 App 配置。

生成本地安卓内测 APK：

```bash
cd /Users/a1/Documents/workspace/myself/peetraining-web/apps/mobile
mkdir -p build
npx eas-cli@latest build --platform android --profile preview --local --output ./build/training-preview.apk
```

根据提示创建或选择 Android 签名 keystore，后续升级同一包必须沿用原签名，妥善保管备份。成功后 `apps/mobile/build/training-preview.apk` 可发给安卓用户安装，不需要 Metro。USB 安装：

```bash
adb install -r ./build/training-preview.apk
```

生成正式 Android App Bundle（AAB）：

```bash
npx eas-cli@latest build --platform android --profile production --local --output ./build/training-production.aab
```

AAB 用于支持该格式的应用商店上传，不能像 APK 一样直接安装。国内渠道如果要求签名 APK，可在后续增加正式 APK profile；不要把 development 调试包当作正式发布包。

生成正式包标识符的 iOS IPA：

```bash
npx eas-cli@latest build --platform ios --profile production --local --output ./build/training-production.ipa
```

首次需选择正确 Apple Developer Team，并创建/选择分发证书和 provisioning profile。在 App Store Connect 创建 Bundle ID 为 `cn.dreamelab.training` 的 App，将其数字 Apple ID 填入 `eas.json` 的 `submit.production.ios.ascAppId`。它不是 Bundle ID，也不是 Apple 登录账号。

上传此 IPA 到 TestFlight：

```bash
npx eas-cli@latest submit --platform ios --profile production --path ./build/training-production.ipa
```

本地构建用 `--path` 指定实际文件，不使用 `--latest` 查找云端构建。也可以在 Xcode Archive 后通过 Organizer 上传。

App Store Connect → TestFlight 等待处理，按实际情况填写加密合规信息，添加内部测试员；外部测试通常还需 Beta App Review。测试员通过苹果 TestFlight 安装。商店签名的 IPA 不能像 APK 一样任意拷贝到 iPhone 安装。

现有 `preview` profile 的 iOS 也是 store 分发，但包名是 `cn.dreamelab.training.preview`。如果要用该 profile，需要为 preview Bundle ID 单独创建 App Store Connect App，并填写 `submit.preview.ios.ascAppId`。建议首次 TestFlight 使用上面的 production profile，避免把两种 Bundle ID 和数字 App ID 混用。

每次提交新版本前检查 `app.config.ts` 的 `version`，以及 EAS/构建产物中的 Android versionCode 和 iOS buildNumber。当前 production 配置带 `autoIncrement`；仍需确认每次上传的构建号大于已上传版本。

## 6. 在 IDE 中手动归档

Expo 的 `run:android` / `run:ios` 没有原生目录时会自动生成。若要专门生成正式变体：

```bash
cd /Users/a1/Documents/workspace/myself/peetraining-web/apps/mobile
APP_VARIANT=production API_BASE_URL=https://training.dreamerlab.cn/api/v1 pnpm exec expo prebuild --platform android
APP_VARIANT=production API_BASE_URL=https://training.dreamerlab.cn/api/v1 pnpm exec expo prebuild --platform ios
```

两种变体共用一份生成目录，从 development 切换到 production 必须重新生成并确认包标识符；不要直接将开发包名归档上传。`--clean` 会删除生成目录中的手动改动，日常不要随意使用。

- Android Studio：打开 `android` → Build → Generate Signed App Bundle or APK → 选择 APK/AAB → 选择正式 keystore → release → 构建。菜单名称可能因版本略有不同，产物位置以 IDE 完成提示为准。
- Xcode：打开 `.xcworkspace` → Target 中确认 Bundle Identifier / Team / Version / Build → 选择通用 iOS 真机目标（不是 Simulator）→ Product → Archive → Organizer → Distribute App → App Store Connect。首次需先在 App Store Connect 建好 App。

项目通过 Expo 插件生成原生配置，需长期保留的原生改动优先写回 `app.config.ts` 或 config plugin。`android/`、`ios/` 当前按 CNG 策略忽略，不直接提交。

## 7. 常见问题

| 现象 | 排查方式 |
| --- | --- |
| 找不到 Android SDK / adb | 检查 SDK Manager 的实际路径、ANDROID_HOME 和 PATH |
| 安卓设备 unauthorized | 手机上允许 USB 调试，重新运行 adb devices |
| xcodebuild requires Xcode | 用 xcode-select 切换到完整 Xcode |
| iOS signing / provisioning 报错 | 核对 Apple Team、Bundle ID、设备或分发类型 |
| 开发包启动后找不到服务 | 启动 Metro；真机和 Mac 保持可互通网络 |
| App 网络错误 | 检查 API_BASE_URL、HTTPS、后端日志；真机 localhost 指手机自身 |
| APK 安装更新失败 | 检查包名、签名是否一致，versionCode 是否递增 |
| 依赖/SDK 版本不匹配 | 先完成 pnpm install，再运行 expo install --check 和 expo-doctor |
| pnpm --filter mobile export 生成 dist | 这是 JS/资源导出，不是 APK、AAB 或 IPA |

首次建议先跑通安卓模拟器和 iOS 模拟器，再安装真机开发包，最后构建独立 APK 与 TestFlight 包。
