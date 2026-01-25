#!/bin/bash

# =================================================================
# 项目名称: kkfly 官方安装脚本
# 描述: 自动获取最新版、下载、安全覆盖安装至 /usr/local/bin
# =================================================================

set -e  # 出错立即停止

# --- 配置区 ---
REPO="kevin197011/kkfly"
BINARY_NAME="kkfly"
INSTALL_PATH="/usr/local/bin/$BINARY_NAME"
TMP_DIR="/tmp/${BINARY_NAME}_install_$(date +%s)"

# --- 1. 环境检查 ---
echo "🔍 正在检查环境..."

if [ "$EUID" -ne 0 ]; then 
    echo "❌ 错误: 请使用 sudo 运行此脚本。"
    exit 1
fi

for cmd in curl tar grep sed; do
    if ! command -v $cmd &> /dev/null; then
        echo "❌ 错误: 系统缺少必要工具: $cmd"
        exit 1
    fi
done

# --- 2. 自动获取最新版本号 ---
echo "🌐 正在检索最新版本..."
LATEST_TAG=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"v?([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    echo "⚠️  无法获取最新版本，尝试回退到默认版本 0.1.5"
    VERSION="0.1.5"
else
    VERSION=$LATEST_TAG
    echo "✨ 发现最新版本: v$VERSION"
fi

# --- 3. 构建下载地址 ---
# 匹配格式: kkfly_0.1.5_linux_amd64.tar.gz
FILENAME="${BINARY_NAME}_${VERSION}_linux_amd64.tar.gz"
URL="https://github.com/$REPO/releases/download/v${VERSION}/${FILENAME}"

# --- 4. 执行下载 ---
mkdir -p "$TMP_DIR"
cd "$TMP_DIR"

echo "📥 正在下载 $FILENAME..."
if ! curl -fsSL "$URL" -o "$FILENAME"; then
    echo "❌ 错误: 下载失败。请检查网络或确认 Release 中存在该文件。"
    echo "URL: $URL"
    exit 1
fi

# --- 5. 解压与校验 ---
echo "📦 正在解压..."
tar -zxf "$FILENAME"

# 自动寻找二进制文件（防止压缩包内包含子目录）
TARGET_FILE=$(find . -type f -name "$BINARY_NAME" -perm -u+x | head -n 1)

if [ -z "$TARGET_FILE" ]; then
    echo "❌ 错误: 在压缩包内未找到可执行文件 $BINARY_NAME"
    exit 1
fi

# --- 6. 安全覆盖安装 ---
echo "🔧 正在执行覆盖安装..."

# 如果程序正在运行，install 命令比 mv 更安全
# 它会断开旧索引并创建新索引，避免 "Text file busy" 错误
install -m 755 "$TARGET_FILE" "$INSTALL_PATH"

# --- 7. 清理与完成 ---
rm -rf "$TMP_DIR"

echo "--------------------------------------------------"
if command -v $BINARY_NAME &> /dev/null; then
    echo "✅ kkfly 安装/更新成功！"
    echo "📍 位置: $INSTALL_PATH"
    echo "🚀 版本: $($BINARY_NAME --version 2>/dev/null || echo "$VERSION")"
    echo "--------------------------------------------------"
    echo "输入 '$BINARY_NAME --help' 开始使用"
else
    echo "❌ 安装失败，请检查 /usr/local/bin 写入权限。"
fi