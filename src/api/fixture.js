import { ApiError } from './http.js'

const TAGS = {
  chat: { id: 'tag-chat', name: '对话', slug: 'chat', dimension: 'capability' },
  local: { id: 'tag-local', name: '本地部署', slug: 'local', dimension: 'capability' },
  agent: { id: 'tag-agent', name: 'Agent', slug: 'agent', dimension: 'capability' },
  creation: { id: 'tag-creation', name: '创作', slug: 'creation', dimension: 'capability' },
  productivity: { id: 'tag-productivity', name: '效率', slug: 'productivity', dimension: 'capability' },
  beginner: { id: 'tag-beginner', name: '入门', slug: 'beginner', dimension: 'capability' },
}

const RESOURCES = [
  {
    id: '6f1c0000-0000-4000-8000-000000000001',
    kind: 'tool',
    slug: 'claude',
    title: 'Claude',
    summary: 'Anthropic 出品的 AI 助手',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.chat],
    primary_category: null,
    quality_score: 80,
    first_published_at: '2026-09-12T12:00:00Z',
    content_updated_at: '2026-09-12T12:00:00Z',
    aliases: [],
    body_markdown: null,
    recommendation_reason: '长文和代码都稳。',
    details: { website_url: 'https://claude.ai', pricing: 'freemium', platforms: ['web'], deployment: ['hosted'] },
    card: { subtitle: 'claude.ai', meta: '免费 + 付费', href: 'https://claude.ai', cta: '访问' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000002',
    kind: 'tutorial',
    slug: 'claude-subscribe',
    title: '如何订阅 Claude 会员',
    summary: '从注册账号到开通 Pro 套餐的完整流程。',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.chat],
    primary_category: null,
    quality_score: 60,
    first_published_at: '2026-09-16T12:00:00Z',
    content_updated_at: '2026-09-16T12:00:00Z',
    aliases: [],
    body_markdown: '通过官方页面完成订阅。',
    recommendation_reason: null,
    details: {
      level: 'beginner',
      minutes: 5,
      steps: ['打开 claude.ai 注册。', '选择 Pro 套餐并付款。'],
      author: '',
      source_url: 'https://claude.ai',
      notes: '请只通过官方页面付款。',
    },
    card: { subtitle: '5 分钟 · 2 步', meta: '入门', href: null, cta: '查看教程' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000003',
    kind: 'repo',
    slug: 'ollama',
    title: 'ollama',
    summary: '一条命令在本地运行开源大模型。',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.local],
    primary_category: null,
    quality_score: 70,
    first_published_at: '2026-09-10T12:00:00Z',
    content_updated_at: '2026-09-10T12:00:00Z',
    aliases: [],
    body_markdown: null,
    recommendation_reason: '本地试模型很快。',
    details: {
      github_repository_id: '',
      full_name: 'ollama/ollama',
      language: 'Go',
      license: 'MIT',
      archived: false,
      last_activity_at: '2026-09-20T00:00:00Z',
    },
    card: { subtitle: 'ollama', meta: 'Go', href: 'https://github.com/ollama/ollama', cta: 'GitHub' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000004',
    kind: 'tutorial',
    slug: 'chatcut-video-agent',
    title: '超简单 ChatCut 使用，Codex大白话剪辑出爆款视频',
    summary: '一个人做出海，工具就是同事。今天这位新同事叫 ChatCut：不发工资、上手快，只有一个要求——你得先开口，把想要的讲清楚。用大白话让 Codex 剪辑出爆款视频。',
    cover_urls: ['/covers/chatcut-1.jpg', '/covers/chatcut-fig1.jpg', '/covers/chatcut-fig2.jpg', '/covers/chatcut-fig3.jpg', '/covers/chatcut-fig4.jpg', '/covers/chatcut-fig5.jpg'],
    cover_fallback_count: 3,
    tags: [TAGS.agent, TAGS.creation, TAGS.productivity, TAGS.beginner],
    primary_category: null,
    quality_score: 95,
    first_published_at: '2026-10-04T13:11:43Z',
    content_updated_at: '2026-10-04T13:11:43Z',
    aliases: ['chatcut'],
    body_markdown: `一个人做出海，工具就是同事。
今天这位新同事叫 ChatCut。不发工资，上手快，只有一个要求——你得先开口，把想要的讲清楚。
**ChatCut 是一个用说话来剪的 AI 剪辑工具。** 把想要什么描述出来，它把片子剪好。
它和剪映不是一回事。剪映是自己动手剪，它替你动手。

#### 目录

- **一、ChatCut 是什么** —— 一句话交代需求，它自己剪
- **二、三个入口，装哪个** —— 官方推荐桌面端，Codex 怎么接上
- **三、界面认一遍** —— 素材、AI 面板、时间线
- **四、第一刀：素材进来，说一句话** —— 从一堆素材到第一版成片
- **五、字幕** —— 改字就是改视频
- **六、导出** —— 顺带说说变成剪映草稿
- **七、还能让它干什么** —— 八条能直接抄的指令
- **八、算一下钱** —— 免费档和 Pro
- **九、它和剪映怎么配合** —— 不是二选一
- **十、优势**
- **常见坑**
- **速查表**

---


### 一、ChatCut 是什么

先说结论：**剪映给的是工具，ChatCut 给的是结果。**
剪映里，你拖素材、切片段、调音量，一帧一帧自己摆。软件负责顺手，动手的是人。
ChatCut 走的是另一条路。把要求说出来——挑高光、排顺序、配画面、加字幕，它先给你一版。
一个是你开车，一个是你说去哪。

#### 它最容易被忽略的一点

**它能导出成可编辑的剪映草稿。**
这句话的分量在这儿：AI 剪出来的东西不会全合心意。导出成剪映草稿，就能回到熟悉的时间线上接着改——而不是被锁在一个还没学会的工具里重来一遍。
就冲这一条，两个工具不用二选一。

---


### 二、三个入口，装哪个

官网地址：https://chatcut.io/   注册推荐大家用个**邮箱或者谷歌账号**直接登录就好了

![插图](/covers/chatcut-fig1.jpg)

官方把入口分成三个：**ChatCut 桌面端、ChatCut 网页端、ChatCut Agent Plugin**。插件那个底下还有两项——Claude Code 和 Codex。

| 入口 | 怎么进 | 适合 |
| :--- | :--- | :--- |
| ChatCut 桌面端 | 网页端登录后，点头像 →「桌面应用」→ 选平台点「下载」 | 官方推荐，大多数剪辑的活 |
| ChatCut 网页端 | app.chatcut.io | 想立刻开始、云端项目、协作 |
| Agent Plugin | 在 Claude Code 或 Codex 桌面应用里新开一个对话安装 | 主要对话留在那边 |

连接任意智能体 如 chatgpt、claude code 、workbuddy 等

\`\`\`plaintext
Call get_active_project through the chatcut_desktop MCP server to connect to "Flying Lavender Anaconda", then read the project, summarize what is currently open, and wait for my instructions. Do not edit anything.
\`\`\`

**新手想先试手，走网页版。** 不用装，打开就能用，把流程跑通再说。
素材在电脑里、或者要往高分辨率出，就换桌面端。
支持的系统只有三个：**macOS（Apple Silicon）、macOS（Intel）、Windows。没有 Linux 版。**
两个版本真正影响日常的差别：

|  | 网页端 | 桌面端 |
| :--- | :--- | :--- |
| 素材 | 要先上传 | 直接读本地文件和文件夹 |
| 导出在哪跑 | 云端 | 本机 |
| 导出的量 | 免费档：所有项目累计 60 分钟 | 不计入那 60 分钟 |

**免费档那 60 分钟是「所有项目加起来」的总量，不是每月重来一次。** 走桌面端导出不烧这个额度——素材多、片子长，这一条就够决定装不装桌面端。
**官方给的建议是：大多数剪辑优先用桌面端。** 它直接读电脑里的文件和文件夹，导出在本机跑，硬件够还能出 4K 和 HDR。

#### Codex / Claude Code 怎么接上

**这一条不用手写配置，官方给的是一句提示词，剩下交给智能体自己干。**
1. 先在 ChatCut 网页端登录
1. 点右上角头像，鼠标停在「Agent Plugin」上
1. 在「Claude Code」或「ChatGPT/Codex」旁边点「复制」
1. 把复制到的那句，粘到对应的桌面应用里发出去
1. 等它弹出 ChatCut 授权页，在上面登录并允许访问
1. **另开一个新会话**——插件工具只在会话开始时加载，安装那一次手里还没有
1. 新会话里明确说一句「用 ChatCut」，再讲你要剪什么
Codex 那边复制到的就是这句：

\`\`\`plaintext
/goal Read chatcut.io/chatgpt to install the ChatCut plugin and set up a new task for me.
\`\`\`

Claude Code 那句一样，只是把网址换成 chatcut.io/claude。WorkBuddy 暂时没有复制按钮，得走它自己的安装指南。
几条容易卡住的：
- **只有桌面应用能装。** Claude 和 ChatGPT 的网页版、远程浏览器工作区、ChatCut 网页端，都不行。
- **授权一次只走一个。** 别同时开两个。
- **别往对话里贴 cookie、access token、账号密码。** 授权页上登一次就够了。
- **插件不会自己更新。** 它来自 Git marketplace，得手动跑更新命令。

\`\`\`bash
# Claude Code
claude plugin marketplace update chatcut-inc
claude plugin update chatcut@chatcut-inc
\`\`\`


\`\`\`bash
# Codex —— marketplace 的名字是它加进来时自己起的，先列出来看
codex plugin marketplace list
codex plugin marketplace upgrade <marketplace-name>
\`\`\`

查有没有接上：

\`\`\`bash
# Codex
codex mcp get chatcut

# Claude Code
claude mcp get plugin:chatcut:chatcut
\`\`\`

还有一条反过来的路：**对话留在 Codex 或 Claude Code 桌面应用里，让它去操作 ChatCut 桌面端打开的那个项目。** 走本机 MCP，三个条件——
- ChatCut 桌面端装过，至少打开过一次
- 另一个应用也在**同一台电脑**上
- ChatCut 桌面端得开着
接上之后，ChatCut 顶栏会多一个智能体图标，点开能看到是谁接的、通没通。显示「Reconnect」就点它重连。本地 MCP 服务是 ChatCut 桌面端自己去认支持的应用注册的，不用手写配置。

---


### 三、界面认一遍

打开编辑器，要认的就几块。

![插图](/covers/chatcut-fig2.jpg)

**我的素材（Library）。** 放视频、图片、音乐。桌面端可以直接读电脑里的文件夹，不用一个个传。
**AI 面板。** 核心在这儿。面板里有 AI 输入框、Agent 选择、Selection Mode、Skills 和 Design Styles 这些。
**Agent 选择**决定这活儿谁来干：ChatCut 自己的智能体、Claude Code、Codex CLI，还有 Hermes Agent。**后三个要等你本机装好了对应的东西才会冒出来**，没装的话它会告诉你还差什么。
用本地 Agent 干活，烧的是那个工具自己的账号和额度，不是 ChatCut 的积分。
**预览区。** 看片子。
**时间线。** V1、V2……是视频轨，A1、A2……是音频轨，上面的轨盖住下面的。**轨道数量不固定，要几条加几条。** AI 剪完照样能手动拖。
**字幕。** 不在侧边栏，在播放控制条上——一个「字幕」的分体按钮，左边按一下开关显隐，右边箭头打开字幕样式。
**在 ChatCut 里，「剪辑」不是一个按钮，是一段对话。** 这是它和剪映最大的界面差别——剪映找功能靠翻菜单，它靠打字。

---


### 四、第一刀：素材进来，说一句话

三步。
1. 素材库点上传，选文件，或者直接拖进窗口
1. AI 输入框里把要求写清楚
1. 让它先出一版，再逐段改
第二步是关键。**要求写得越具体，第一版越接近你要的。**

\`\`\`plaintext
帮我把这 12 段素材剪成一条 60 秒的视频，
挑出最精彩的镜头，加 B-roll，配一段轻快的音乐。
\`\`\`

它给的第一版叫初剪。**初剪不是成品，是用来改的。** 别指望一次到位。
改有两条路：
- **继续说。**「这段太长」「换个有节奏的音乐」「开头那段删掉」
- **上手改。** 在时间线上手动拖
两条路混着走最省事：大结构靠说，小细节靠手。

---


### 五、字幕

**字幕**：带有时间码、可在视频中同步显示的文字。 **文字稿**：语音识别出来的文字内容，通常记录说了什么。
**在 ChatCut 里，字幕是文字，不是画面。改字就是改视频。**
内置样式有 Plain、Netflix、TikTok Pop 这些，选中就能套；自己调顺手的能存成「已保存样式」，下次直接用。
真正省时间的是**文本式剪辑**：把转录稿调出来，选中不要的一段，按退格——视频里那段跟着剪掉，后面的空隙自动合上。转录稿有段落和片段两种视图，片段视图里还能拖着调整顺序。
停顿也能统一。转录轨上有「Gap」和「停顿」两个控制，能把停下来太久的地方理顺。**它只能还原录音里本来就有的停顿，变不出没录进去的静音。**
**读一遍稿子改错字，比在时间线上找句子快得多。** 这是这一类工具的核心卖点，也是从剪映切过来之后最不一样的手感。

---


### 六、导出

导出在本机完成（桌面端），要什么选什么：
- 视频文件
- 音频
- 字幕文件
- 动态图形
- 可重新链接的 NLE 素材归档
**还有一条路值得单独说：把整个时间线变成剪映专业版（或 CapCut）的可编辑草稿。这条路只有桌面端有。**
流程是：
1. 在桌面端打开项目，点顶部的**导出**
1. 中文界面会看到「剪映」这一栏，英文界面是「CapCut」——前提是这台机器装过对应编辑器
1. 装了不止一个，在**应用**里选目标
1. 想连动态图形一起带过去，在**包含**里打开「动态图形」。**这一项可能要 Pro**，而且它会把图形渲染成透明片段
1. 出现「部分内容不受支持」就展开看一眼：**已导入**的没问题，**不受支持**的那些要在目标编辑器里重建
1. 导出结束，&#28857;**「打开剪映专业版」**。剪映本来就开着的话，&#28857;**「重启剪映专业版」**，新草稿才会显示

![插图](/covers/chatcut-fig3.jpg)

**注意：它交出去的是一份草稿工程，不是渲染好的 MP4。** ChatCut 独有的属性不保证能传过去，交付前在剪映的时间线上过一遍。
导出按钮如果是灰的，先把剪映打开一次，让它把草稿目录建出来，再回来重开导出设置。

---


### 七、还能让它干什么

按场景列，每条后面是能直接照着说的指令。
**找高光**

\`\`\`plaintext
把这 8 段录屏里的重点找出来，剪成一条 90 秒的教程
\`\`\`

**口播转成片**

\`\`\`plaintext
这是我的一段口播视频，把停顿理顺，加上字幕
\`\`\`

**做动态图形**

\`\`\`plaintext
给这条视频加一段章节动画，标出三个要点
\`\`\`

它做的动画是**可编辑的**，不是一段烧死的视频。章节条、柱状图、饼图、时间线、关键词打字，这几类都能生成。
**配画面**

\`\`\`plaintext
这几句在讲一个人做站，给我配几个对应的空镜
\`\`\`

生成镜头支持参考图——给一张图，它照着那个感觉出画面。
**配乐**

\`\`\`plaintext
给这条视频配一段不抢人声的轻音乐
\`\`\`

**生成封面**

\`\`\`plaintext
生成一张封面图，风格简洁，留出标题的位置
\`\`\`

**搜素材**

\`\`\`plaintext
素材库里有没有城市夜景的片段
\`\`\`

**打包带走**

\`\`\`plaintext
打包成剪映草稿，我要在剪映里接着调
\`\`\`

**实际使用效果**，比上篇本地工具生成好很多，特别chatcut 的音色和配乐好很多！ 以下为 chatcut 生成效果，仅简单测试效果，真实生产会复杂些！

---


### 八、算一下钱

ChatCut 分两档：**Free 和 Pro**。
Free 能用完整的编辑器，AI 功能按账户里显示的额度来。Pro 带定期积分，解锁更多生成和交付能力。
**手动剪辑不烧积分。** 拖时间线、剪片段、传素材、翻项目，这些都不消耗。烧积分的是 AI 处理和生成——生成视频、图片、音乐这一类。
一个建议：**先用免费档把流程完整跑一遍**，确定它真能省下时间，再谈充多少。
**具体价格和积分数量，以账户里的计费页为准。** 官方文档本身不写死数字，因为调价快。查余额走头像菜单 → 看套餐和余额 → 积分记录，能按天、按类型、按明细筛。
还有一条容易踩：**导出不消耗积分，所以单买积分包也解不开免费档那 60 分钟的网页导出额度**——那个要的是有效的付费套餐。
也可以在智能体如ChatGpt 上，安装 ChatCut 插件来做视频，或者在ChatCut登录ChatGpt， 可以先行这样使用！要求比较高一定要用到chatcut 积分时，再充钱！

---


### 九、它和剪映怎么配合

这两样不是二选一。 桌面版 chatcut 可以和 剪映联动

![插图](/covers/chatcut-fig4.jpg)

**ChatCut 负责第一版，剪映负责最后那 10%。**
一条片子，最花时间的不是剪辑手法，是把几百段素材过一遍、挑出能用的、排个顺序。这一段交给 ChatCut。
剩下的精修——转场卡在鼓点上、字幕往上挪两个像素——在剪映里做，手比打字准。
导出成剪映草稿这条路，就是为这个流程铺的。

---


### 十、优势

ChatCut 资源库非常丰富，比剪映更新颖有特色更吸引人

![插图](/covers/chatcut-fig5.jpg)


### 常见坑

**它要联网。** 「本地素材」说的是素材不用上传，不是说全部功能都能离线跑。生成类的活儿仍然要联网。
**素材显示缺失，别重新导入。** 重装之后素材找不到，用「重新关联文件」指向原文件。重新导入会产生两份，白占空间。
**项目在你的账号里，不在硬盘上。** 换设备登录同一个账号，项目还在。但本地素材的路径是某台机器上的，换机器要重新指一次。
**网页端导出的额度是累计的。** 免费档「全部项目累计 60 分钟」，不按月重置。导出本身不烧积分，所以买积分包也解不开它，得升级套餐。
**免费档的 AI 额度，取决于生成多少。** 手动剪辑不烧积分；一上来就大量生成视频，额度掉得比想象快。
**剪映草稿的导出按钮是灰的。** 先启动一次剪映专业版，让它把草稿目录建出来，再回来重开导出设置。
**初剪不满意是正常的。** 它给的是起点，不是成品。要求写得越具体，起点越靠近终点。

---


### 速查表


| 想干什么 | 对着它说 |
| :--- | :--- |
| 剪第一版 | 把这批素材剪成一条 60 秒的视频，挑高光，配轻音乐 |
| 找重点 | 把这几段里的重点找出来，剪成一条 90 秒的 |
| 去停顿 | 把停顿理顺，加上字幕 |
| 加动画 | 加一段章节动画，标出三个要点 |
| 配画面 | 这几句在讲某个主题，配几个对应的空镜 |
| 配乐 | 配一段不抢人声的轻音乐 |
| 生成封面 | 生成一张封面图，留出标题位置 |
| 搜素材 | 素材库里有没有某个风格的片段 |
| 打包 | 打包成剪映草稿 |


---


### 最后

四条记下来。
**一、它是「先说后剪」，不是「先剪后说」。** 把要求讲清楚，比会用它的每个按钮重要得多。
**二、第一版一定是粗糙的。** 它的价值在替你省掉挑选和排列那一段，不在一次做对。
**三、它和剪映是一对。** AI 出初剪，人在剪映里精修，导出成剪映草稿这条路就是为这个准备的。**注意它交出去的是草稿工程，不是成片。**
**四、字幕改的是文字，不是画面。** 这是它和剪映手感差别最大的地方，值得单独花十分钟摸一遍。
免费档先试，跑通了再充钱。#chatcut  #codex   #AI剪辑
我是想风@xaiwind，一个关注出海与 AI、AIGC 的创作者。
本文基于AI辅助与个人实践融合创作，如果文章对你有帮助，欢迎点赞、收藏、关注。
本人文章持续维护修正，有问题欢迎联系或者评论区见。每天进步一点，我们终会到达目的地！

#### 附录·本文参考

- ChatCut 官方文档·桌面端：https://chatcut.io/zh/docs/desktop-app
- 网页端、Desktop 和 Agent Plugin：https://chatcut.io/docs/chatcut-products
- Agent Plugin 安装（Claude Code / Codex 接入）：https://chatcut.io/docs/agent-plugin
- 导出为剪映 / CapCut 草稿：https://chatcut.io/docs/export-capcut-jianying
- 转录与字幕：https://chatcut.io/zh/docs/transcript-and-captions
- 时间线：https://chatcut.io/zh/docs/timeline
- 网页端导出限额：https://chatcut.io/docs/web-export-limits
- 套餐与积分：https://chatcut.io/zh/docs/plans-and-credits
- ChatCut 官网：https://chatcut.io
往期优秀视频文章
**超简单剪映使用，从小白到用Skill，剪辑第一个爆款视频**
https://x.com/xaiwind/status/2106324762347880468
**超简单的火柴人视频,  5步教会你用ChatGPT和Grok制作**
https://x.com/xaiwind/status/2097984612198760469
**录屏剪辑软件推荐**
https://x.com/xaiwind/status/2097594816616116417`,
    recommendation_reason: '大白话就能驱动视频剪辑，一人出海做视频内容的神器。',
    details: {
      level: 'beginner',
      minutes: 8,
      steps: [
        '安装 ChatCut 插件与 MCP 端点：在 Codex 或 Claude Code 配置 ChatCut 官方 MCP 服务，并完成账户授权连接。',
        '准备原始音视频素材：将要剪辑的录屏、口播或演示视频放入项目目录，让 Agent 获取素材文件上下文。',
        '大白话下达剪辑指令：用日常口语描述需求（如“去除开头无声和口误、保留干货，按快节奏短视频剪出前 30 秒”）。',
        'AI 智能生成字幕与动态包装：让 Agent 自动识别语音生成时间轴对齐的精美字幕，并根据高潮点插入动态贴字与转场。',
        '预览工程与一键成片导出：在 ChatCut 预览窗口检查剪辑轨道，微调后通过指令由 Agent 自动渲染并导出高清视频。',
      ],
      author: '想风 (@xaiwind)',
      source_url: 'https://x.com/xaiwind/status/2106734008436707477',
      notes: '大白话指令越具体，AI 剪辑越符合预期；建议明确视频受众与平台风格（如 TikTok/YouTube Shorts 快节奏）。大工程建议按镜头或章节分段让 AI 加工，以保持最高剪辑精度。',
    },
    card: { subtitle: '想风 (@xaiwind) · 8 分钟', meta: '入门', href: 'https://x.com/xaiwind/status/2106734008436707477', cta: '查看教程' },
    featured: true,
  },
]

function notFound() {
  return new ApiError({ status: 404, code: 'not_found', message: 'not found', requestId: 'fixture' })
}

function matches(item, { kind, q, tag }) {
  if (kind && item.kind !== kind) return false
  const tags = Array.isArray(tag) ? tag : tag ? [tag] : []
  if (tags.length && !tags.every((slug) => item.tags.some((entry) => entry.slug === slug))) return false
  const text = q.trim().toLowerCase()
  if (!text) return true
  const haystack = [
    item.title,
    item.summary,
    item.body_markdown,
    ...(item.details?.steps || []),
    ...item.tags.map((entry) => entry.name),
  ].filter(Boolean).join(' ').toLowerCase()
  return haystack.includes(text)
}

function effectiveSort(sort, q) {
  if (sort === 'relevance' && !q.trim()) return 'recommended'
  if (sort) return sort
  return q.trim() ? 'relevance' : 'recommended'
}

function page(items, sort) {
  return {
    items,
    next_cursor: null,
    has_more: false,
    total: null,
    effective_sort: sort,
    ranking_version: null,
    ranking_computed_at: null,
    applied_query: { kind: items[0]?.kind || '', q: '', tags: [], sort },
  }
}

export function createFixtureClient() {
  return {
    async listResources(params = {}) {
      const q = params.q || ''
      const sort = effectiveSort(params.sort, q)
      let items = RESOURCES.filter((item) => matches(item, { kind: params.kind, q, tag: params.tag }))
      if (sort === 'latest') {
        items = items.slice().sort((a, b) => Date.parse(b.first_published_at) - Date.parse(a.first_published_at))
      }
      if (params.cursor) return page([], sort)
      return page(items.map(publicResource), sort)
    },
    async getBySlug(slug) {
      const found = RESOURCES.find((item) => item.slug === slug)
      if (!found) throw notFound()
      return publicResource(found)
    },
    async listTags(params = {}) {
      const items = RESOURCES.filter((item) => matches(item, { kind: params.kind, q: params.q || '', tag: '' }))
      const map = new Map()
      items.forEach((item) => {
        item.tags.forEach((tag) => {
          const prev = map.get(tag.slug) || { ...tag, count: 0 }
          prev.count += 1
          map.set(tag.slug, prev)
        })
      })
      return { tags: [...map.values()] }
    },
    async listFeatured(kind) {
      const items = RESOURCES.filter((item) => item.kind === kind && item.featured).map((item, index) => ({
        ...publicResource(item),
        position: index + 1,
      }))
      return { items }
    },
    async postEvents(events) {
      return { accepted: events?.length || 0, duplicate: 0 }
    },
  }
}

function publicResource(item) {
  const resource = { ...item }
  delete resource.featured
  return resource
}
