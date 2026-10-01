#!/usr/bin/env bash
# 把 EAS 打好的安卓内测包上传到 OSS，生成下载链接（内测期安卓从这里安装，PRD 3.3）。
# 用法：scripts/upload-apk.sh <apk 文件> <版本号>
#   需要本机装好 ossutil 并配置有上传权限的 RAM 子账号；APK_OSS_BUCKET 为公共读的发布桶（只放安装包）。
#   上传后把下载链接填到后台 7.8「App 版本」，0.6 / 0.6b 版本更新弹窗会跳到这里。
set -euo pipefail
APK=${1:?apk 文件}; VERSION=${2:?版本号}
BUCKET=${APK_OSS_BUCKET:?需要设置 APK_OSS_BUCKET}
KEY="android/training-${VERSION}.apk"
ossutil cp "${APK}" "oss://${BUCKET}/${KEY}" --meta "Content-Type:application/vnd.android.package-archive"
ossutil cp "${APK}" "oss://${BUCKET}/android/training-latest.apk" --meta "Content-Type:application/vnd.android.package-archive" -f
ENDPOINT=${APK_OSS_ENDPOINT:-oss-cn-beijing.aliyuncs.com}
echo "下载链接：https://${BUCKET}.${ENDPOINT}/${KEY}"
echo "最新版固定链接：https://${BUCKET}.${ENDPOINT}/android/training-latest.apk"
