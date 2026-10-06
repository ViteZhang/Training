# 本地调试指南（Mac 电脑 + 安卓手机 / iPhone）

这份指南写给不熟悉开发工具的同学，按顺序一步一步做就行。遇到任何一步和描述不一样，把**终端里最后二三十行文字**复制给 Claude，它会帮你看。

## 先了解整体是怎么连起来的

```
 ┌──────────────── 你的 Mac ────────────────┐
 │  Docker：MySQL 数据库 + Redis 缓存        │
 │  后端服务（Go）       端口 8080           │◀─── 同一个 Wi-Fi ───▶  手机上的「考研Training 开发」App
 │  App 开发服务器（Metro）端口 8081         │
 │  管理后台（可选）     端口 5173           │
 └──────────────────────────────────────────┘
```

- 后端、数据库都跑在你的 Mac 上，手机通过 Wi-Fi 访问 Mac
- 本地不会真的发短信、不会真的调用 AI 或支付，全部是「模拟」的，**登录验证码会打印在 Mac 的终端里**
- 手机 App 不能用「Expo Go」扫码打开，要在 Mac 上编译一个「开发版」装到手机上（第一次 10～30 分钟，之后改代码不用重新装）

建议顺序：第 1～4 步做一次准备 → 第 5 步装到安卓手机 → 跑通后再做第 6 步装 iPhone。

---

## 第 1 步：打开「终端」

后面所有命令都在「终端」里输入。

1. 按 `⌘ 空格`（Command + 空格），输入 `终端` 或 `Terminal`，回车
2. 出现一个白色或黑色的窗口，这就是终端
3. **怎么执行命令**：把本指南灰色框里的命令复制、粘贴到终端，按回车。一次粘一个框
4. 需要「新开一个终端窗口」时：在终端里按 `⌘ T`（开一个新标签页）
5. 要停止正在运行的东西：在那个窗口里按 `Control + C`

> 输入密码时屏幕上不显示任何字符，这是正常的，输完直接回车。

---

## 第 2 步：安装需要的软件（只做一次）

### 2.1 Homebrew（Mac 上装开发软件的工具）

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

- 会让你输入 Mac 的开机密码，然后按回车确认
- 装完后，终端最后会出现 **Next steps**，下面有两三行以 `echo` 或 `eval` 开头的命令，**把它们复制下来依次执行**（这一步漏了，后面会提示 `brew: command not found`）
- 如果下载很慢或失败，把报错发给 Claude，可以改用国内镜像

### 2.2 Go、Node、pnpm、CocoaPods

```bash
brew install go node cocoapods
npm install -g pnpm@10
```

国内网络建议再执行下面两行，让下载走国内镜像（否则后面可能很慢）：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
npm config set registry https://registry.npmmirror.com
```

### 2.3 Docker Desktop（用来跑数据库）

1. 浏览器打开 <https://www.docker.com/products/docker-desktop/>，点 **Download for Mac**
   - 不知道选哪个：点屏幕左上角苹果图标 → 关于本机，「芯片」写 Apple M1/M2/M3/M4 就选 **Apple Silicon**，写 Intel 就选 **Intel**
2. 打开下载的 `.dmg`，把 Docker 拖进「应用程序」
3. 在「应用程序」里打开 Docker，同意协议，登录可以跳过（Skip）
4. 屏幕顶部菜单栏出现一个小鲸鱼图标，并且 Docker 窗口左下角显示 **Engine running**（绿色）就好了

> 以后每次调试前都要先打开 Docker Desktop。

### 2.4 Android Studio（给安卓手机编译 App 用）

1. 打开 <https://developer.android.com/studio>，下载 Mac 版（同样按芯片选），拖进「应用程序」
2. 打开 Android Studio，一路选 **Next**，安装类型选 **Standard**，最后点 **Finish**，等它下载完组件（几 GB，要一会儿）
3. 回到终端，执行下面这段，让终端找得到 Android 的工具：

```bash
cat >> ~/.zshrc <<'EOF'
export ANDROID_HOME=$HOME/Library/Android/sdk
export PATH=$PATH:$ANDROID_HOME/emulator:$ANDROID_HOME/platform-tools
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
EOF
source ~/.zshrc
```

### 2.5 Xcode（给 iPhone 编译 App 用，先做安卓可以晚点装）

1. 打开 Mac 的 App Store，搜索 **Xcode**，安装（十几 GB，时间较长）
2. 装完打开一次 Xcode，同意协议，弹出要安装组件（含 iOS）就点安装
3. 回到终端执行（要输入开机密码）：

```bash
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
```

### 2.6 检查是否都装好了

关掉终端再重新打开一个，执行：

```bash
go version && node -v && pnpm -v && docker -v && adb version
```

每一行都打印出版本号就说明安装成功。哪一行报 `command not found`，就是那个软件没装好。

---

## 第 3 步：把代码下载到 Mac（只做一次）

用 GitHub Desktop 最简单：

1. 打开 <https://desktop.github.com/> 下载安装，用你的 GitHub 账号登录
2. 菜单 **File → Clone Repository**，在列表里选 **ViteZhang/Training**，下面的保存位置保持默认（`/Users/你的用户名/Documents/GitHub/Training`），点 **Clone**
3. 左上角 **Current Branch** 选 `main`

以后要拿最新代码：在 GitHub Desktop 里点 **Fetch origin**，再点 **Pull origin**。

然后在终端安装前端依赖（第一次要几分钟）：

```bash
cd ~/Documents/GitHub/Training/peetraining-web
pnpm install
```

---

## 第 4 步：启动后端

### 4.1 查出你 Mac 的局域网地址（IP）

```bash
ipconfig getifaddr en0
```

会打印类似 `192.168.1.23` 的地址。**下文所有的 `你的IP` 都换成这个地址。**

- 什么都没打印：试试 `ipconfig getifaddr en1`
- 换了 Wi-Fi 或重启路由器后 IP 可能会变，变了要重新查

### 4.2 启动

先确认 Docker Desktop 已经打开并且显示 Engine running，然后：

```bash
cd ~/Documents/GitHub/Training/peetraining-server
OSS_MOCK_BASE_URL=http://你的IP:8080 make dev
```

- 第一次会下载数据库镜像和 Go 依赖，可能要 5～15 分钟
- 看到一行含 `"msg":"api listening"` 的文字，就说明后端启动成功了
- **这个终端窗口不要关**，它要一直开着；验证码也会打印在这里
- 如果 Mac 弹窗问「是否允许 api 接受传入网络连接」，点 **允许**

> `OSS_MOCK_BASE_URL` 是给 App 上传图片（拍照导题等）用的，不加的话手机上传图片会失败。

### 4.3 检查后端能不能访问

1. 在 Mac 浏览器里打开 <http://localhost:8080/api/v1/health>，看到 `"status":"ok"` 就对了
2. **用手机浏览器**（手机连和 Mac 同一个 Wi-Fi）打开 `http://你的IP:8080/api/v1/health`，也应该看到 `"status":"ok"`

第 2 条打不开的话，App 一定也连不上，先解决它：

- 确认手机和 Mac 连的是同一个 Wi-Fi（公司访客网络、酒店网络通常不让设备之间互相访问）
- Mac「系统设置 → 网络 → 防火墙」如果是打开的，先关掉试试
- 实在不行：用手机开热点，让 Mac 连手机热点，再重新查一次 IP

### 4.4 停止后端

在运行 `make dev` 的窗口按 `Control + C`。想把数据库也停掉（不删数据）：

```bash
cd ~/Documents/GitHub/Training/peetraining-server
make dev-down
```

---

## 第 5 步：把 App 装到安卓手机

### 5.1 手机打开「USB 调试」（只做一次）

1. 手机「设置 → 关于手机」，找到 **版本号**，连续点 7 次，提示「您已处于开发者模式」
   - 小米：「设置 → 我的设备 → 全部参数」里点「OS 版本」/「MIUI 版本」
   - 华为 / 荣耀：「设置 → 关于手机 → 版本号」
   - OPPO / vivo / 一加：「设置 → 关于手机 → 版本信息 → 版本号」
2. 回到设置，搜索「开发者选项」，打开 **USB 调试**
   - 小米还要打开 **USB 安装** 和 **USB 调试（安全设置）**
3. 用数据线把手机连到 Mac，手机弹出「允许 USB 调试吗？」，勾选「始终允许」，点 **允许**
4. 终端检查：

```bash
adb devices
```

下面出现一行设备编号，后面写着 `device` 就对了。写 `unauthorized` 说明手机上还没点允许；什么都没有就换根数据线（有的线只能充电）。

### 5.2 编译并安装

开一个**新的终端窗口**（`⌘ T`，后端那个窗口别关），执行：

```bash
cd ~/Documents/GitHub/Training/peetraining-web/apps/mobile
API_BASE_URL=http://你的IP:8080/api/v1 npx expo run:android --device
```

- 如果问你选哪台设备，用方向键选你的手机，回车
- 第一次编译 10～30 分钟，期间会刷很多文字，正常
- 手机可能弹出「是否允许安装」，点允许
- 装好后手机上会自动打开 **考研Training 开发**
- **这个窗口也要开着**，它是 App 的开发服务器；你改了代码，手机上会自动刷新

### 5.3 登录

1. App 里输入任意 11 位手机号（例如 `13900000001`），勾选同意协议，点获取验证码
2. 回到**后端那个终端窗口**，按 `⌘ F` 搜索 `mock sms code`，找到最新的一行，里面的 `"code":"294327"` 这 6 位数字就是验证码
3. 填进 App 登录。新手机号会自动注册

> 同一个手机号 60 秒内只能获取一次验证码，一天最多 10 次。测试时可以多换几个号码。

---

## 第 6 步：把 App 装到 iPhone

### 6.1 先确认苹果开发者账号

App 用到了推送通知，**免费的 Apple ID 签不了名**，需要公司的付费苹果开发者账号（Apple Developer Program）：

- 让账号管理员在 <https://developer.apple.com> 的 **People** 里把你的 Apple ID 加进团队
- 然后打开 Xcode → 菜单 **Xcode → Settings → Accounts**，点左下角 **+** → **Apple ID**，登录你的 Apple ID
- 公司账号还没开通的话，先告诉 Claude，可以临时做一个去掉推送的版本来调试

### 6.2 iPhone 设置（只做一次）

1. 用数据线连到 Mac，iPhone 弹出「要信任此电脑吗？」→ **信任**，输入锁屏密码
2. iPhone「设置 → 隐私与安全性」，拉到最下面打开 **开发者模式**，按提示重启手机
   - 看不到这个选项：先打开一次 Xcode 并保持 iPhone 连着，再去找

### 6.3 编译并安装

先把安卓那个开发服务器窗口按 `Control + C` 停掉（两个会抢同一个端口），然后：

```bash
cd ~/Documents/GitHub/Training/peetraining-web/apps/mobile
API_BASE_URL=http://你的IP:8080/api/v1 npx expo run:ios --device
```

- 选你的 iPhone，回车；问开发团队（Team）时选公司的团队
- 第一次会先安装 iOS 依赖（CocoaPods），再编译，总共 10～30 分钟
- 装好后打开 App，弹出「允许查找并连接到本地网络上的设备」时**一定点允许**，否则连不上 Mac
- 登录方法和安卓一样（第 5.3 步）

---

## 第 7 步（可选）：打开管理后台

1. 先创建一个后台账号（后端在运行时，新开终端窗口执行，只做一次）：

```bash
cd ~/Documents/GitHub/Training/peetraining-server
ADMIN_INITIAL_PASSWORD='Admin-pass-2026' go run ./cmd/api create-admin admin 管理员 13800000000 admin
```

看到 `"msg":"admin created"` 就成功了。

2. 启动管理后台：

```bash
cd ~/Documents/GitHub/Training/peetraining-web
pnpm --filter admin dev
```

3. 浏览器打开 <http://localhost:5173/admin/>，账号 `admin`，密码 `Admin-pass-2026`
4. 要输入两步验证码时，去后端窗口 `⌘ F` 搜索 `admin 2fa code`，取里面的 `"code"`
5. 第一次登录会要求改密码，改完记住新密码

---

## 以后每天怎么调试

不需要再编译安装，按下面做就行：

1. 打开 Docker Desktop，等到 Engine running
2. 终端窗口 1（后端）：

```bash
cd ~/Documents/GitHub/Training/peetraining-server
OSS_MOCK_BASE_URL=http://你的IP:8080 make dev
```

3. 终端窗口 2（App 开发服务器）：

```bash
cd ~/Documents/GitHub/Training/peetraining-web/apps/mobile
API_BASE_URL=http://你的IP:8080/api/v1 npx expo start --dev-client
```

4. 打开手机上的「考研Training 开发」，它会自动连上 Mac；没连上的话，用手机相机扫终端里显示的二维码
5. 改了代码会自动刷新；在开发服务器窗口按 `r` 可以手动刷新 App

**需要重新编译安装（重做第 5.2 / 6.3 步）的情况**：新增或升级了原生组件（package.json 里的依赖变了）、改了 `app.config.ts`。拿不准的话，问 Claude 就行。

---

## 常见问题

| 现象 | 原因和解决 |
| --- | --- |
| `make dev` 报 `Cannot connect to the Docker daemon` | Docker Desktop 没打开，打开后等到 Engine running 再执行 |
| `make dev` 报 `port is already allocated` / `address already in use` | 上次没关干净。执行 `make dev-down`，再重新 `make dev`；还不行就重启 Mac |
| App 一直转圈、提示网络错误 | 先用手机浏览器打开 `http://你的IP:8080/api/v1/health`（第 4.3 步）。打不开就是网络问题；能打开就检查 IP 有没有变、启动 App 时的 `API_BASE_URL` 有没有写对 |
| App 红屏，写 `Unable to load script` / 连不上开发服务器 | 开发服务器窗口没开，执行「以后每天怎么调试」第 3 步 |
| 验证码不对 | 只用**最新**一条 `mock sms code`；一个码 5 分钟内有效，错 5 次失效，重新获取 |
| 获取验证码提示「操作太频繁」 | 60 秒限制或当天 10 次已用完，换个手机号 |
| 安卓编译报 `SDK location not found` 或 `JAVA_HOME` | 第 2.4 步第 3 点没执行，或者执行后没重开终端 |
| iPhone 编译报 `Signing` / `Provisioning profile` / `Personal development teams do not support the Push Notifications capability` | 没有公司开发者账号的权限，见第 6.1 步 |
| iPhone 上 App 打开后连不上 | 「设置 → 隐私与安全性 → 本地网络」里打开「考研Training 开发」 |
| 以上都不是 | 把终端最后二三十行文字复制给 Claude |
