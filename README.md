<a href="https://github.com/3899/netease-cloud-music">
  <img src="https://socialify.git.ci/3899/netease-cloud-music/image?description=1&descriptionEditable=%E7%BD%91%E6%98%93%E4%BA%91%E9%9F%B3%E4%B9%90%20Golang%20API%20%E6%8E%A5%E5%8F%A3%20%2B%20%E5%91%BD%E4%BB%A4%E8%A1%8C%E5%B7%A5%E5%85%B7%E5%A5%97%E4%BB%B6%20%2B%20%E4%B8%80%E9%94%AE%E5%AE%8C%E6%88%90%E6%AF%8F%E6%97%A5%E4%BB%BB%E5%8A%A1&font=Source%20Code%20Pro&logo=https%3A%2F%2Fp6.music.126.net%2Fobj%2Fwo3DlcOGw6DClTvDisK1%2F62177614927%2F22ad%2F1953%2Fa6cf%2Fe7007953d5942445a0444ca346bd06be.png%3Fraw%3Dtrue&name=1&owner=1&pattern=Floating%20Cogs&theme=Auto" alt="netease-cloud-music" />
</a>

<div align="center">
  <br/>

  <div>
    <a href="./LICENSE">
      <img
        src="https://img.shields.io/github/license/3899/netease-cloud-music?style=flat-square"
      />
    </a >
    <a href="https://github.com/3899/netease-cloud-music/releases">
      <img
        src="https://img.shields.io/github/v/release/3899/netease-cloud-music?style=flat-square"
      />
    </a >
    <a href="https://github.com/3899/netease-cloud-music/releases">
      <img
        src="https://img.shields.io/github/downloads/3899/netease-cloud-music/total?style=flat-square"
      />  
    </a >
  </div>
  
</div>

# 🎵 netease-cloud-music

> 🎖 本项目基于开源项目 [netease-cloud-music](https://github.com/chaunsin/netease-cloud-music) 完成二次开发与功能拓展，谨向原作者及开源社区致以诚挚谢意。

## 本次新增功能说明

### 使用前先登录

`playids`、`task --playids`、`sign`、`scrobble` 这类命令都需要先登录。

最常用的是 Cookie 登录：

```shell
# 直接导入 Cookie 字符串
ncmctl login cookie 'MUSIC_U=xxx; __csrf=yyy; NMTID=zzz;'

# 或从文件导入
ncmctl login cookie -f cookie.txt
```

如果已经有 CookieCloud，也可以直接登录：

```shell
ncmctl login cookiecloud -u <你的UUID> -p <你的密码> -s http://127.0.0.1:8088
```

登录成功后，再执行 `playids` 或 `task --playids`。

### `playids` 指定歌曲完整播放

`playids` 用于对指定 `songId` 池执行真实完整播放，模拟 Android 客户端行为。

适用场景：

- 单账号定向播放指定歌曲
- 需要重复播放固定歌曲池，而不是从榜单自动选歌
- 需要在账号已满级时仍继续执行播放任务

#### 模拟真实客户端行为

基于 Android 逆向分析，实现了以下仿真特性：

| 特性 | 说明 |
|:--|:--|
| **三阶段 WebLog 上报** | `startplay` → `play-begin` → `play-complete`，与 Android 端一致 |
| **EAPI 加密通信** | 使用 `EAPI` 加密通道进行播放，与 Android 端相同 |
| **设备指纹持久化** | 自动生成 Android 设备身份（deviceId、型号、系统版本等），持久化到数据库，跨会话复用 |
| **CDN 音频缓存** | 同一首歌在单次任务中仅下载一次，后续播放走缓存，上报 `download=1` |
| **播放时长抖动** | 上报时长添加 ±0~3 秒随机偏移，避免精确整数秒 |
| **结束类型混合** | 85% `playend`（正常结束）、10% `ui`（手动切歌）、5% `interrupt`（中断） |
| **自然间隔分布** | 歌曲间隔采用幂函数分布，偏向较短间隔，更接近真实用户行为 |

#### 独立的每日播放上限

`playids` 拥有独立的每日计数器（与 `scrobble` 完全分离），支持配置区间范围：

- 每天首次运行时，在 `[dailyMin, dailyMax]` 区间内随机确定当天目标
- 持久化到数据库，当天后续运行复用同一目标值
- 每天的播放次数不完全相同，更加自然

默认区间为 `50~100`，可通过配置文件或命令行参数修改。

#### 命令格式

```shell
ncmctl playids --ids <songId列表> [--ids-file <文件>] [--num <数量>] [--gap-min <秒>] [--gap-max <秒>] [--daily-min <下限>] [--daily-max <上限>]
```

#### 参数说明

| 参数 | 说明 | 默认值 |
|:--|:--|:--|
| `--ids` | 直接传入 songId 列表，支持逗号、空格分隔 | 空 |
| `--ids-file` | 从文件读取 songId 列表 | 空 |
| `--num` | 本次最多播放多少首（0 = 播到今日上限为止） | `0` |
| `--gap-min` | 两首歌之间最小随机间隔秒数 | `5` |
| `--gap-max` | 两首歌之间最大随机间隔秒数 | `20` |
| `--daily-min` | 每日播放次数下限（含） | `50`（读取配置文件） |
| `--daily-max` | 每日播放次数上限（含） | `100`（读取配置文件） |

> 💡 **优先级：** 命令行参数 > 配置文件 > 代码默认值

#### 使用示例

```shell
# 直接传入多个songId，播到今日上限
ncmctl playids --ids 3373818852,3373845775,3372894655,3370932775

# 从文件读取songId
ncmctl playids --ids-file ./song_ids.txt

# 本次只播 10 首
ncmctl playids --ids 3370932775 --num 10

# 临时指定今日上限为 200~250
ncmctl playids --ids 3370932775 --daily-min 200 --daily-max 250

# 不插入歌曲间隔，便于快速验证
ncmctl playids --ids 3370932775 --num 1 --gap-min 0 --gap-max 0
```

`song_ids.txt` 示例：

```text
3373818852
3373845775
3372894655
3372989897
3370932775
3370931988
```

#### 行为说明

- 输入支持 `--ids` 和 `--ids-file` 同时给出，运行前会合并并去重
- 歌曲每轮会随机打乱，池耗尽后允许重复播放
- `playids` 拥有独立的每日计数，**不再与 `scrobble` 共享上限**
- 如果某首歌不可播放、拉流失败或上报失败，该首会记失败，但整批任务会继续执行
- 当歌曲池数量小于目标时，会自动开始下一轮随机打乱继续播放

#### 执行过程

1. 读取并去重歌曲池
2. 加载/生成设备指纹，注入 Cookie
3. 确定今日播放上限（首次运行随机，后续复用）
4. 查询歌曲详情与时长
5. 通过 EAPI 获取播放 URL
6. **Phase 1**: 上报 `startplay` 事件
7. 模拟缓冲延迟（100~500ms）
8. **Phase 2**: 上报 `play-begin` 事件
9. 拉取音频流（首次从 CDN，后续走缓存）
10. 等待至歌曲时长完成（含 ±0~3s 抖动）
11. **Phase 3**: 上报 `play-complete` 事件（含时长、结束类型）

#### 典型日志

```text
[playids] 当前账号: uid=123456789 昵称="张三"
[playids] 设备指纹: 设备=Xiaomi 17 Pro Max, 系统=Android 15, 版本=9.5.0, 渠道=netease, deviceId=A1B2C3D4...
[playids] 任务开始: 歌曲池=3首, 目标播放=73首, 今日上限=73首, 今日已完成=0首, 今日剩余=73首, 间隔=5s-20s
[playids] 歌曲池[1]: songId=3370932775 歌名="歌A" 时长=3m17s
[playids] 正在播放: 第1/73首, 第1轮第1首, songId=3370932775, 歌名="歌A", 时长=3m17s
[playids] Phase1 startplay: songId=3370932775, 结果=成功
[playids] Phase2 play-begin: songId=3370932775, 缓冲耗时=327ms
[playids] Phase2 play-begin: songId=3370932775, 结果=成功
[playids] 拉流完成: songId=3370932775, 来源=CDN, 已耗时=3s, 补等待=3m14s, end=playend
[playids] Phase3 play-complete: songId=3370932775, 结果=成功, 上报时长=199s, end=playend, download=0
[playids] 本首结果: 第1/73首, 成功, songId=3370932775, 歌名="歌A"
[playids] 播放间隔: 下一首等待 7s
...
[playids] 正在播放: 第4/73首, 第2轮第1首, songId=3370932775, 歌名="歌A", 时长=3m17s
[playids] Phase1 startplay: songId=3370932775, 结果=成功
[playids] Phase2 play-begin: songId=3370932775, 缓冲耗时=215ms
[playids] Phase2 play-begin: songId=3370932775, 结果=成功
[playids] 拉流完成: songId=3370932775, 来源=缓存, 已耗时=3s, 补等待=3m15s, end=playend
[playids] Phase3 play-complete: songId=3370932775, 结果=成功, 上报时长=198s, end=playend, download=1
...
[playids] 执行完成: 目标=73首, 成功=73首, 失败=0首
[playids] 今日统计: 执行前=0首, 执行后=73首, 今日上限=73首
```

#### 配置文件

在 `config.yaml` 中可以配置 playids 每日上限和设备指纹（可选）：

```yaml
# PlayIDs 播放任务配置
# 每天首次运行时，在 [dailyMin, dailyMax] 区间内随机确定当天目标
playids:
  dailyMin: 50
  dailyMax: 100

# 设备指纹配置（可选）
# 建议从自己手机的网易云音乐 APP 抓包中提取真实设备信息填入
# device:
#   deviceId: "从Cookie中的deviceId字段获取"
#   os: "android"
#   osVer: "14"
#   appVer: "9.5.0"
#   channel: "netease"
#   mobileName: "Pixel 8"
#   resolution: "1080x2400"
```

> 💡 **提示：** 设备指纹不配置时会自动生成随机值并持久化，每个账号独立维护。配置文件中填写的字段会覆盖自动生成值。

### `task` 对 `playids` 的显式支持

`playids` 不属于默认任务，只有显式传 `--playids` 才会启用。

命令格式：

```shell
ncmctl task --playids [--playids.ids <songId列表>] [--playids.ids-file <文件>] [--playids.cron "<cron表达式>"]
```

```shell
# 每天按默认 cron 执行 playids
ncmctl task --playids --playids.ids-file ./song_ids.txt

# 自定义 cron 和每日上限
ncmctl task --playids --playids.ids 3373818852,3373845775 --playids.cron "0 19 * * *" --playids.daily-min 80 --playids.daily-max 120
```

注意：

- `task` 默认不会自动带上 `playids`
- 必须显式传 `--playids`
- 开启 `--playids` 但没有提供 `playids.ids` 或 `playids.ids-file` 时会直接报错

### Docker 用法

当前仓库建议直接使用 GitHub Container Registry 镜像：

```shell
docker pull ghcr.io/3899/netease-cloud-music:latest
```

如果使用 Docker，建议把歌曲文件挂载进容器，然后显式调用 `playids` 或 `task --playids`：

```shell
docker run --rm -it \
  -v ${PWD}/data:/root \
  -v ${PWD}/song_ids.txt:/root/song_ids.txt:ro \
  ghcr.io/3899/netease-cloud-music:latest \
  /app/ncmctl playids --ids-file /root/song_ids.txt
```

或：

```shell
docker run --rm -it \
  -v ${PWD}/data:/root \
  -v ${PWD}/song_ids.txt:/root/song_ids.txt:ro \
  ghcr.io/3899/netease-cloud-music:latest \
  /app/ncmctl task --playids --playids.ids-file /root/song_ids.txt
```

### `playids` 与 `scrobble` 的区别

|     命令     |   类型   | 每日上限 | 说明 |
|:----------:|:------:|:----:|:---|
| `scrobble` | 单次任务/定时 | 固定 300 | 从榜单里选歌并上报播放，包含满级退出逻辑与单曲去重 |
| `playids`  | 单次任务/定时 | 可配置(50~100) | 播放指定 `songId` 池，模拟 Android 客户端行为，三阶段上报，独立计数 |

## 以下为原作者文档

---
[![GoDoc](https://godoc.org/github.com/chaunsin/netease-cloud-music?status.svg)](https://godoc.org/github.com/chaunsin/netease-cloud-music) [![Go Report Card](https://goreportcard.com/badge/github.com/chaunsin/netease-cloud-music)](https://goreportcard.com/report/github.com/chaunsin/netease-cloud-music) [![ci](https://github.com/chaunsin/netease-cloud-music/actions/workflows/ci.yml/badge.svg)](https://github.com/chaunsin/netease-cloud-music/actions/workflows/ci.yml) [![deploy image](https://github.com/chaunsin/netease-cloud-music/actions/workflows/deploy_image.yml/badge.svg)](https://github.com/chaunsin/netease-cloud-music/actions/workflows/deploy_image.yml)

> 🚀 网易云音乐 Golang API 接口 + 命令行工具套件 + 一键完成每日任务

## ⚠️ 重要声明

> **📅 2025-06-03 更新：**
> 目前风控极为严格，刷歌功能存在较高封号风险，不建议使用。如执意使用并收到 [非法挂机行为警告](https://github.com/chaunsin/netease-cloud-music/issues/34)
> ，请立即终止，否则后果自负！

- 🚫 **本项目仅供个人学习使用，切勿用于商业用途或非法用途！**
- ⚖️ **使用本项目遇到封号等问题概不负责，使用前请谨慎考虑！**
- 📧 **如有侵权请联系删除！**

---

## ✨ 功能特性

### 🖥️ 命令行工具 (ncmctl)

#### 🔐 登录方式

- [x] 📷 扫码登录
- [x] 🍪 Cookie 方式登录
- [x] ☁️ [CookieCloud](https://github.com/easychen/CookieCloud/blob/master/README_cn.md) 方式登录
- [x] ~~📱 短信登录~~ (存在风控问题)
- [x] ~~🔑 手机号密码登录~~ (存在风控问题)

#### 📋 每日任务

- [x] 🎯 一键完成每日任务（音乐合伙人、云贝签到、VIP 签到、刷歌 300 首）
- [x] 💰 云贝签到（支持自动领取签到奖励）
- [x] 🎤 "音乐合伙人"自动测评
    - 5 首基础歌曲 + 2~7 首随机额外歌曲测评（不包含"歌曲推荐"测评）
    - 📢 2025 年 3
      月 [公告](https://music.163.com/#/event?id=30336457500&uid=7872690377) | [规则](https://y.music.163.com/g/yida/9fecf6a378be49a7a109ae9befb1b8d3)
- [x] 🎧 每日刷歌 300 首（支持去重功能）
- [x] 💎 VIP 每日签到

#### ☁️ 云盘功能

- [x] ☁️ 云盘上传（支持并行批量上传）

#### 🎶 音乐处理

- [x] 🔓 解密 `.ncm` 文件为 `.mp3`/`.flac` 可播放格式（支持并行批量解析）
- [x] 📥 音乐下载，支持多种品质

|   品质   |         别名          | 说明      |
|:------:|:-------------------:|:--------|
|   标准   |  `standard`、`128`   | 128kbps |
|  高品质   |   `higher`、`192`    | 192kbps |
|   极高   | `exhigh`、`HQ`、`320` | 320kbps |
|   无损   |   `lossless`、`SQ`   | FLAC    |
| Hi-Res |    `hires`、`HR`     | 高解析度    |

#### 🛠️ 调试工具

- [x] 🔐 `crypto` 子命令 - 接口参数加解密，便于调试
- [x] 🌐 `curl` 子命令 - 调用网易云音乐 API，无需关心参数加解密
    - [ ] 支持动态链接请求

#### 🔜 计划中

- [ ] VIP 日常任务完成（待考虑）
- [ ] "音乐人"任务自动完成（待考虑）
- [ ] 🌐 Proxy 代理支持

### 📦 API 接口

|   类型    | 适用场景        |
|:-------:|:------------|
| `weapi` | 网页端、小程序（推荐） |
| `eapi`  | PC 端、移动端    |

> 💡 **提示：** 目前主要实现了 `weapi`
> ，接口相对较全，推荐使用。如需其他接口可提 [Issue](https://github.com/chaunsin/netease-cloud-music/issues)。

---

## 💻 环境要求

|    依赖    |   版本要求   |  必需  |
|:--------:|:--------:|:----:|
|  Golang  | \>= 1.24 |  ✅   |
| Makefile |    -     | ❌ 可选 |
|   Git    |    -     | ❌ 可选 |
|  Docker  |    -     | ❌ 可选 |

---

## 🔨 安装指南

### 方式一：下载预编译版本

直接从 [Releases](https://github.com/chaunsin/netease-cloud-music/releases) 页面下载对应平台的二进制文件。

### 方式二：源码安装

```shell
# 直接安装
go install github.com/chaunsin/netease-cloud-music/cmd/ncmctl@latest

# 或者克隆后安装
git clone https://github.com/chaunsin/netease-cloud-music.git
cd netease-cloud-music && make install
```

> 📂 默认安装路径：`$GOPATH/bin`

### 方式三：Docker 安装

```shell
# Docker Hub
docker pull chaunsin/ncmctl:latest

# GitHub Container Registry
docker pull ghcr.io/chaunsin/ncmctl:latest
```

> 📖 Docker 使用文档：https://hub.docker.com/r/chaunsin/ncmctl

**自行编译镜像：**

```shell
git clone https://github.com/chaunsin/netease-cloud-music.git
cd netease-cloud-music && make build-image
```

> ⚠️ 自行编译需安装 Docker 环境，国内网络建议使用代理。

### 方式四：青龙面板

详见 👉 [青龙脚本安装指南](docs/qinglong.md)

---

## 🚀 使用指南

### 📱 一、登录

支持 5 种登录方式，详情如下：

<details>
<summary>🔐 点击展开登录方式详情</summary>

#### 1️⃣ 短信登录

```shell
ncmctl login phone 188xxx8888
```

成功发送短信后会提示：

```shell
send sms success
please input sms captcha:
```

输入收到的短信验证码即可完成登录。

> ⚠️ **注意事项：**
> 1. 短信发送每日有限制，请勿频繁登录以免触发风控
> 2. 若长时间未收到短信，可能是运营商延迟，可尝试重新发送或稍后再试

---

#### 2️⃣ 手机号密码登录

需先在网易云音乐中开启手机号密码登录权限。

```shell
ncmctl login phone 188xxx8888 -p 123456
```

> ⚠️ 此方式可能触发 `8821 需要行为验证码验证` 错误，仅作备选方案。
>
> 🔒 **请勿泄露密码！**

---

#### 3️⃣ Cookie 登录

当正常登录失败时，Cookie 登录可作为保底方案。

可通过浏览器插件获取
Cookie，推荐 [Cookie Editor](https://chromewebstore.google.com/detail/cookie-editor/ookdjilphngeeeghgngjabigmpepanpl)。

```shell
# 方式一：直接导入 Cookie 字符串
ncmctl login cookie 'cookie字符串内容'

# 方式二：从文件导入
ncmctl login cookie -f cookie.txt
```

**支持的文件格式：**

- `header` 格式
- `json` 格式
- [netscape 格式](https://docs.cyotek.com/cyowcopy/1.10/netscapecookieformat.html)

> 📖 详细说明请查看 `ncmctl login cookie -h`

---

#### 4️⃣ CookieCloud 登录

[CookieCloud](https://github.com/easychen/CookieCloud/blob/master/README_cn.md) 是一款浏览器 Cookie 管理插件，支持自动同步
Cookie 到云端并加密存储。

**操作流程：**

1. 📥 安装 CookieCloud 浏览器插件
2. ⚙️ 完成插件配置
3. 🎵 在网页端登录网易云音乐
4. 🔄 点击【手动同步】按钮同步到云端
5. 🖥️ 执行登录命令

```shell
ncmctl login cookiecloud -u <用户名> -p <密码> -s http://0.0.0.0:8088
```

> ⚠️ **注意事项：**
> 1. 请确保服务端地址、账号、密码正确
> 2. 若出现 Cookie 找不到错误，请在插件中手动同步或重新登录后重试
> 3. 使用第三方服务器请自行评估安全风险

---

#### 5️⃣ ~~二维码登录~~（已弃用）

> ⚠️ 由于网易云风控严重，扫码登录会出现 `8821 需要行为验证码验证`
> 错误，暂不支持。详见 [Issue #26](https://github.com/chaunsin/netease-cloud-music/issues/26)

```shell
ncmctl login qrcode
```

</details>

---

### 📋 二、每日任务

**一键执行所有每日任务：**

```shell
ncmctl task
```

**默认包含的任务：**

|     任务     | 说明            |
|:----------:|:--------------|
|   `sign`   | 云贝签到 + VIP 签到 |
| `partner`  | 音乐合伙人         |
| `scrobble` | 刷歌 300 首      |

**选择性执行任务：**

```shell
# 仅执行签到
ncmctl task --sign

# 执行签到和刷歌（无音乐合伙人资格时）
ncmctl task --sign --scrobble
```

**自定义执行时间：**

```shell
# 设置刷歌任务在每天 20:00 执行
ncmctl task --scrobble.cron "0 20 * * *"
```

> 💡 **提示：**
> - 需要先登录
> - 本命令以服务方式持续运行，退出请按 `Ctrl+C`
> - 采用标准 [crontab](https://zh.wikipedia.org/wiki/Cron) 表达式，[在线编写工具](https://crontab.guru/)

> ⚠️ **警告：** 签到任务默认关闭自动领取奖励功能（存在封号风险），如需开启请添加 `--sign.automatic` 参数。

---
### 📥 三、音乐下载

#### 下载单曲

```shell
# 通过分享链接下载 Hi-Res 品质
ncmctl download -l hires 'https://music.163.com/song?id=1820944399'

# 通过歌曲 ID 下载
ncmctl download -l hires '1820944399'

# 下载无损品质到指定目录
ncmctl download -l SQ 'https://music.163.com/song?id=1820944399' -o ./download/
```

#### 批量下载

```shell
# 下载整张专辑（并发数 5，最大 20）
ncmctl download -p 5 'https://music.163.com/#/album?id=34608111'

# 下载歌手所有歌曲（严格模式：无对应品质则跳过）
ncmctl download --strict 'https://music.163.com/#/artist?id=33400892'

# 下载歌单
ncmctl download 'https://music.163.com/playlist?id=593617579'
```

> 💡 **提示：**
> - 默认下载到 `./download` 目录，音质为无损 (SQ)
> - `--strict` 严格模式下，无指定品质则跳过；否则会降级下载

---

### ☁️ 四、云盘上传

```shell
# 上传单个文件
ncmctl cloud '/path/to/music.mp3'

# 批量上传目录
ncmctl cloud '/path/to/music/'
```

**参数说明：**

|  参数  | 默认值 | 最大值 | 说明    |
|:----:|:---:|:---:|:------|
| `-p` |  3  | 10  | 并发上传数 |

> ⚠️ 目录深度不能超过 3 层。更多过滤条件请查看 `ncmctl cloud -h`。

---

### 🔓 五、NCM 文件解密

将加密的 `.ncm` 文件转换为可播放的 `.mp3`/`.flac` 格式。

```shell
# 批量解析目录
ncmctl ncm '/path/to/ncm/files' -o ./output

# 设置并发数
ncmctl ncm '/path/to/ncm/files' -o ./output -p 10
```

> ⚠️ 目录深度不能超过 3 层。

---

### 🛠️ 六、其他命令

```shell
# 查看帮助
ncmctl -h
```

---

## 📚 API 使用示例

|  功能  | 示例文件                                                                 | 说明  |
|:----:|:---------------------------------------------------------------------|:----|
|  登录  | [example_login_test.go](example/example_login_test.go)               | -   |
| 云盘上传 | [example_cloud_upload_test.go](example/example_cloud_upload_test.go) | 需登录 |
| 音乐下载 | [example_download_test.go](example/example_download_test.go)         | 需登录 |

---

## ❓ 常见问题

### Q1: 下载无损音乐品质不准确？

当指定 `-l lossless` 时，可能会下载到 Hi-Res 品质。若歌曲不支持 Hi-Res，则会正常下载无损。此问题仍在排查中。

### Q2: 每日刷歌为什么达不到 300 首？

`scrobble` 支持去重功能，会在 `$HOME/.ncmctl/database/` 记录已听歌曲。

**可能原因：**

1. 使用本程序前已听过的歌曲未记录，导致重复播放不计数
2. Top 榜单歌曲数量有限，新歌更新不及时

> ⚠️ **建议：不要清理 `$HOME/.ncmctl/database/` 目录下的数据！**

### Q3: `task` 和 `scrobble`、`sign`、`partner` 子命令有什么区别？

|             命令              |  类型  | 说明                    |
|:---------------------------:|:----:|:----------------------|
|           `task`            |  服务  | 包含所有子命令，定时执行，适合部署到服务器 |
| `scrobble`/`sign`/`partner` | 单次任务 | 立即执行并返回结果             |

---

## ❤️ 致谢

### 👥 贡献者

- [sjpqxuzdly03646](https://github.com/sjpqxuzdly03646) - "音乐合伙人"功能支持
- [stkevintan](https://github.com/stkevintan) - CookieCloud 登录方式

### 📦 参考项目

- [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)
- [pyncm](https://github.com/mos9527/pyncm)
- [musicdump](https://github.com/naruto2o2o/musicdump)
- [crontab.guru](https://crontab.guru)

感谢所有依赖的开源项目！
