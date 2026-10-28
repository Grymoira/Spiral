# Spiral · 跨网络 P2P 连通性研究记录

> 状态：**Research / Proposal（研究中，未采纳为架构决策）**  
> 记录日期：2026-10-28  
> 审查代码基线：[`main@ac9eb9a`](https://github.com/Grymoira/Spiral/commit/ac9eb9af47be58bb5867ecdb06814706584cd645)  
> 追踪：[Issue #70](https://github.com/Grymoira/Spiral/issues/70)；相关 [#56](https://github.com/Grymoira/Spiral/issues/56)、[#61](https://github.com/Grymoira/Spiral/issues/61)、[#11](https://github.com/Grymoira/Spiral/issues/11)、[#65](https://github.com/Grymoira/Spiral/issues/65)  
> **证据边界：本文件是对公开标准、论文、开源示例与现有架构的文献／源码审查，尚未编译 P2P PoC、运行 NAT 仿真、进行两台异网设备连通性测试或声学验收。**

## 1. 目标、术语和不能承诺的事情

**产品研究目标**：用户不租用第三台 Spiral 协调服务器，房主本人的电脑作为房间权威；其他客户端尽可能经由端到端加密的直接路径接入。房间消息不携带音频字节、绝对本地路径、私密媒体 URL、邀请密钥或其他会话凭据日志。

必须明确以下三种目标的差别：

| 目标 | 含义 | 实际可行边界 |
| --- | --- | --- |
| 无专属协调 VPS | 不用开发者或用户自建第三台专属服务器 | 可以用房主权威 + 带外信令；可能仍使用公共 STUN |
| 无流量中继 | 连线成功后控制消息在两个端点之间直传 | 有条件可行；不能保证穿透所有 NAT 与防火墙 |
| 零第三方网络服务 | 不用 STUN、TURN、DHT、在线信令等外部服务 | 仅在存在已知可达端点及可用地址族／端口映射等条件下可实际保证 |

一个短房间代码本身**不能**自动从互联网定位另一台 NAT 后面的电脑。ICE 需要交换候选信息；可先借助用户信任的带外渠道（手动复制、二维码或已有聊天工具）交换，再尝试直接连接。带外聊天工具依然可能属于第三方服务，但不充当 Spiral 房间控制通道。

若两侧都处于无法被访问的严格网络条件，既没有共同可达端点，也没有可用中继，就**无法保证**建立直连。产品必须报告“不支持此网络组合／直连失败”，不得悄悄切换到未告知的 TURN／Relay。

## 2. 已验证的公开研究证据

### 2.1 大规模测量（不能泛化为 Spiral 成功率）

[Trautwein 等，2026，Large-Scale Measurement of NAT Traversal for the Decentralized Web: A Case Study of DCUtR in IPFS](https://arxiv.org/abs/2604.12484)：超过 440 万次穿透尝试、8.5 万多个不同网络、167 国。论文报告：**先成功取得 relay reservation 且完成公有地址发现之后**，IPFS/libp2p DCUtR 打洞阶段的**条件成功率约 70% ± 7.1%**；成功连接中 97.6% 首次尝试完成，并观察 TCP 与 QUIC 在其样本中的成功率近似。该研究**不**证明任意两台 Spiral 电脑有 70% 的纯两端直连机会，也不证明无 relay 的发现阶段能正常运转。

配套：[ProbeLab 的 DCUtR 分析](https://github.com/probe-lab/dcutr-project/blob/main/docs/dcutr-final-report.md) 进一步将实验按 NAT 行为分层，提示建立 relay reservation 本身就可能失败。跨研究样本、测量方法和入组条件不同，统计结果不能简单相乘或转为产品 SLA。

### 2.2 标准的边界

- [RFC 5128 — State of Peer-to-Peer Communication across NATs](https://www.rfc-editor.org/rfc/rfc5128)：UDP/TCP 打洞有具体 NAT 条件，不保证对所有映射／过滤行为生效。
- [RFC 8445 — ICE](https://www.rfc-editor.org/rfc/rfc8445)：端点收集 host／server-reflexive／relayed 候选、交换候选并做连接性检查；**信令交换机制不是 ICE 自身提供的**。
- [RFC 5780 — NAT Behavior Discovery](https://www.rfc-editor.org/rfc/rfc5780)：当前 NAT 行为探测不能保证另一目的地址或未来时刻有相同映射。
- [RFC 6887 — Port Control Protocol](https://www.rfc-editor.org/rfc/rfc6887)：当网关确实支持且用户授权时可申请端口映射；不能穿越不受控的所有上游 CGNAT。
- [RFC 8827 — WebRTC Security Architecture](https://www.rfc-editor.org/rfc/rfc8827)：DTLS 加密不自动认证带外信令的来源；应将对端指纹／公钥与受信邀请绑定或进行带外核对。

## 3. 可复现的开源代码候选

| 代码／文档 | 能证明什么 | 不能证明什么 |
| --- | --- | --- |
| [libdatachannel C++ copy-paste 示例](https://github.com/paullouisageneau/libdatachannel/tree/master/examples/copy-paste) | 无专属信令服务时，手动交换 offer、answer、candidate，建立 DTLS DataChannel、收发消息，查询 selected candidate pair 与 RTT | 示例能编译运行不等于跨两个 NAT 打洞成功；其 [answerer.cpp](https://github.com/paullouisageneau/libdatachannel/blob/master/examples/copy-paste/answerer.cpp) 的 STUN 配置默认被注释 |
| [libdatachannel BUILDING.md](https://github.com/paullouisageneau/libdatachannel/blob/master/BUILDING.md) | 支持 Windows/MSVC 编译，`NO_MEDIA` 和 `NO_WEBSOCKET` 可裁剪与本目标无关的功能 | 不是 Spiral 的 CI 或 Windows 实机验收 |
| [Pion ICE](https://github.com/pion/ice)／[Pion WebRTC](https://github.com/pion/webrtc) | Go 生态可执行的 ICE、WebRTC 与可靠 DataChannel 候选栈 | ICE-only 并不是完整的可靠加密控制通道 |
| [Pion data-channels 示例](https://github.com/pion/webrtc/blob/main/examples/data-channels/main.go)／[pion-to-pion](https://github.com/pion/webrtc/tree/main/examples/pion-to-pion) | 前者是**浏览器 ↔ Go** 的 SDP 复制示例，后者更接近两个 Go 端点；应分别核对具体示例入口和依赖 | 不应将浏览器↔Go 示例误写为两个纯 Go 命令行程序 |
| [libp2p DCUtR](https://libp2p.io/docs/dcutr/) | 已部署的中继协调后尝试直连机制，可借鉴多网络路径切换 | 需要初始 relay／发现基础设施，不等同严格零第三方 |
| [saorsa-testnet NAT Emulation](https://github.com/saorsa-labs/saorsa-testnet/blob/main/docs/infrastructure/NAT-EMULATION.md) | Docker/NAT 拓扑与回归实验设计参考 | 页面中的 expected success rates **不是** Spiral 实测；仅有 `iptables --random-fully` 不足以证明完整 endpoint-dependent NAT 映射语义 |

**技术选型尚未决定**。第一轮优先实验：C++ + libdatachannel（便于 Qt 原型集成）；对照实验：Go + Pion（便于未来沿用 Go 的房间权威）。不得为了试验而把未经审计的新依赖直接引入生产构建。先审查许可证、链接方式、Windows 发行与维护成本。

## 4. Spiral 的最小架构改动原则

保留当前 [ADR-0002](../adr/0002-coordinated-realtime-sync.md) 的中心**逻辑**权威，不强制保持第三台中心**物理**服务器。房主电脑可在未来运行应用私有 Go 房间协调进程；Qt 负责本地文件加载与音频输出。连接适配器在充分测试后才考虑在既有 TLS/WSS 与新 P2P DataChannel 之间切换。

任何新传输都必须遵守：

1. 五人上限（含房主），优先 host-star 拓扑，先不做无主 mesh 共识；房主失联要明确撤销播放权威。
2. 保持房间代际、状态修订、幂等 operationId、ARM/ACK/COMMIT、cancel ACK、撤销与 fail-closed；P2P 建连成功**绝不是**已验证同步出声。
3. 所有跨公网控制通道经过身份绑定的端到端加密；不关闭证书验证，不将现有 loopback-only 明文 HTTP 原封不动暴露到网络。
4. 睡眠／唤醒、网络切换、ICE restart、对端离开和房主崩溃后必须失效旧待执行事件，取得新权威快照、重新校时后才能继续。
5. 同步准确度的真机声学 P50/P95/max 仍由 [#11](https://github.com/Grymoira/Spiral/issues/11) 独立验收；时钟品质由 [#65](https://github.com/Grymoira/Spiral/issues/65) 处理。

正式落地前须写一份新的 **Proposed ADR**，明确本地 Go 权威与 Qt 的 IPC、候选传输栈、信令安全、失败回退、许可/分发以及是否有可选中继；本研究笔记本身**不是已接受的 ADR**。

## 5. 测试拓扑、步骤和证据级别

| 阶段 | 拓扑与操作 | 最低证据 | 绝不宣称 |
| --- | --- | --- | --- |
| A · 单机 | 同一 Windows PC 两个独立实例、手动 Offer/Answer/Candidate，交换随机消息 | 构建环境、库版本／commit、接通日志、加密会话及双向消息摘要 | 跨网穿透成功 |
| B · 受控 NAT | 独立 network namespaces、NAT 网关、抓包/conntrack；注入 RTT、loss、端口映射变更及断线 | 实际映射/过滤行为、候选类型、连接结果、可重复测试脚本 | 模拟 NAT 等于真实运营商 |
| C · 实际异网 | 家宽与移动数据等两个独立出口，不通过同一代理；一端可能有 CGNAT | 出口与地址族的脱敏描述、ICE 状态、候选分类、延迟、是否确为 direct 而非 relay | 已经跨国验收 |
| D · 跨国真实网络 | 两台真实设备，来自不同国家和网络；云端公有地址测试单独标记 | 地区／ISP类别、受限网络条件、反复测试统计、故障恢复与失败样本 | 公有 IPv4 VPS 测试等同双家庭 NAT 或跨国必连 |

阶段 B 的网络规则必须用 packet capture 与 conntrack 检查；WSL2 虚拟网络不等于 Windows 真实网络行为。

每个实验至少记录：**构建版本、源码 SHA、参与设备、时间、连接路径（direct IPv6 / direct IPv4 / srflx / relay / failed）、是否使用 STUN/信令/TURN、候选收集结果、ICE 状态与耗时、DataChannel 是否真实加密和双向传输、重复试验次数及明确失败原因**。隐私安全日志不得包含原始音频路径、token、邀请码或未脱敏的 SDP/candidate/IP；原始敏感诊断若确需采集，只能经过参与者同意并采用受控本地保存。

## 6. 下一步决策门槛

- **先做 PoC，不改主播放协议**：以 C++ copy-paste 完成 A；B 加入 STUN、手动信令、严格 NAT 失败，记录构建与连接证据；C 才算真实异网；D 才算真实跨国。
- **通过 A 不足以批准 P2P 融入 Spiral**；至少完成 B、C 并经过安全和原生 Windows 评审；产品要承诺跨国可用则必须完成 D。
- 若样本只证明在可达 IPv6/IPv4 成功，则把能力描述为“在可达公网端点间直连”，不要承诺通用 NAT 打洞。
- 如严格 NAT 无法直连，明确表示失败；第三方中继如成为新需求，应单独决策并且让用户知情。
- 没有真实 PoC 执行数据时，**Issue #70 持续 Open；本文件不得作为功能实现／测试通过的证据**。

## 7. 研究来源与维护规则

此文档的标准、论文与开源项目链接经 2026-10-28 公开来源核对；不代表其全部源码已经审计或已由 Spiral 本机运行。候选依赖应锁定具体版本／commit 后再执行 PoC，并将 test artifacts、失败案例、实际网络拓扑、许可证审计与技术决策回填 [#70](https://github.com/Grymoira/Spiral/issues/70)。不要只保存在聊天记录中。

新的、独立可复现缺陷：先检查既有 Issues 后再建单独 Issue。一般探索记录：更新本文档并在 #70 关联 commit/PR。架构决策：新增 Proposed ADR，评审后才标 Accepted。实际代码改动：独立小 PR + CI + 对应设备验收；**不得以研究笔记、自动化 CI 或仿真结果替代真机声学证据**。
