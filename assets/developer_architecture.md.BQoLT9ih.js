import{c as a,Q as n,j as p,m as t}from"./chunks/framework.B1f5Gm19.js";const k=JSON.parse('{"title":"架构设计","description":"","frontmatter":{"title":"架构设计"},"headers":[],"relativePath":"developer/architecture.md","filePath":"developer/architecture.md","lastUpdated":1779163937000}'),e={name:"developer/architecture.md"};function i(l,s,d,c,o,r){return n(),p("div",null,[...s[0]||(s[0]=[t(`<h1 id="架构设计" tabindex="-1">架构设计 <a class="header-anchor" href="#架构设计" aria-label="Permalink to &quot;架构设计&quot;">​</a></h1><p>本文从高层视角介绍 Trojan-Go-Next 的系统设计和数据流。</p><h2 id="核心设计原则" tabindex="-1">核心设计原则 <a class="header-anchor" href="#核心设计原则" aria-label="Permalink to &quot;核心设计原则&quot;">​</a></h2><p>Trojan-Go-Next 的核心设计是<strong>可插拔隧道栈</strong>（Pluggable Tunnel Stack）。每个功能模块（TLS、WebSocket、Trojan 协议、路由等）都实现为独立的隧道层，通过组合不同的隧道层构成完整的代理功能。</p><div class="language- vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang"></span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>┌─────────────────────────────────────────┐</span></span>
<span class="line"><span>│              代理模式 (Proxy)             │</span></span>
<span class="line"><span>│  client / server / forward / nat / custom │</span></span>
<span class="line"><span>├─────────────────────────────────────────┤</span></span>
<span class="line"><span>│              隧道栈 (Tunnel Stack)       │</span></span>
<span class="line"><span>│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐   │</span></span>
<span class="line"><span>│  │入站层│→│协议层│→│加密层│→│传输层│   │</span></span>
<span class="line"><span>│  └──────┘ └──────┘ └──────┘ └──────┘   │</span></span>
<span class="line"><span>├─────────────────────────────────────────┤</span></span>
<span class="line"><span>│           基础设施 (Infrastructure)      │</span></span>
<span class="line"><span>│  config / log / metric / statistic      │</span></span>
<span class="line"><span>└─────────────────────────────────────────┘</span></span></code></pre></div><h2 id="五种代理模式" tabindex="-1">五种代理模式 <a class="header-anchor" href="#五种代理模式" aria-label="Permalink to &quot;五种代理模式&quot;">​</a></h2><p>每种模式是不同的隧道栈组合：</p><table tabindex="0"><thead><tr><th>模式</th><th>入站</th><th>出站</th><th>用途</th></tr></thead><tbody><tr><td>CLIENT</td><td>socks+http</td><td>隧道栈</td><td>标准客户端</td></tr><tr><td>SERVER</td><td>tls/ws</td><td>freedom/router</td><td>标准服务端</td></tr><tr><td>FORWARD</td><td>dokodemo</td><td>隧道栈</td><td>端口转发</td></tr><tr><td>NAT</td><td>tproxy</td><td>隧道栈</td><td>透明代理</td></tr><tr><td>CUSTOM</td><td>自定义</td><td>自定义</td><td>完全控制</td></tr></tbody></table><h2 id="客户端数据流-示例" tabindex="-1">客户端数据流（示例） <a class="header-anchor" href="#客户端数据流-示例" aria-label="Permalink to &quot;客户端数据流（示例）&quot;">​</a></h2><p>一个典型的客户端连接（启用了 mux + shadowsocks + websocket）：</p><div class="language- vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang"></span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>应用程序</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   SOCKS5/HTTP</span></span>
<span class="line"><span>│ adapter  │ ──────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   Trojan 协议封装</span></span>
<span class="line"><span>│ trojan   │ ──────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   smux 多路复用</span></span>
<span class="line"><span>│ mux      │ ──────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   Shadowsocks AEAD</span></span>
<span class="line"><span>│ shadowsocks│ ─────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   TLS 加密</span></span>
<span class="line"><span>│ tls      │ ──────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   WebSocket 帧</span></span>
<span class="line"><span>│ websocket│ ──────────────→</span></span>
<span class="line"><span>└─────────┘</span></span>
<span class="line"><span>   │</span></span>
<span class="line"><span>   ▼</span></span>
<span class="line"><span>┌─────────┐   TCP 传输</span></span>
<span class="line"><span>│ transport│ ──────────────→  远端服务器</span></span>
<span class="line"><span>└─────────┘</span></span></code></pre></div><h2 id="服务端数据流" tabindex="-1">服务端数据流 <a class="header-anchor" href="#服务端数据流" aria-label="Permalink to &quot;服务端数据流&quot;">​</a></h2><p>服务端使用<strong>分支树</strong>结构，同时支持直接 TLS 和 WebSocket 两种接入方式：</p><div class="language- vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang"></span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span>                    TLS 监听 (443)</span></span>
<span class="line"><span>                        │</span></span>
<span class="line"><span>              ┌─────────┴──────────┐</span></span>
<span class="line"><span>              │                     │</span></span>
<span class="line"><span>        直接 TLS 连接          WebSocket 连接</span></span>
<span class="line"><span>              │                     │</span></span>
<span class="line"><span>              ▼                     ▼</span></span>
<span class="line"><span>         TLS 解密            WS + TLS 解密</span></span>
<span class="line"><span>              │                     │</span></span>
<span class="line"><span>              └─────────┬──────────┘</span></span>
<span class="line"><span>                        │</span></span>
<span class="line"><span>                  Trojan 协议解析</span></span>
<span class="line"><span>                  /    │    \\</span></span>
<span class="line"><span>              密码错误  密码正确  非Trojan</span></span>
<span class="line"><span>                  │      │       │</span></span>
<span class="line"><span>              回退到    路由     代理到</span></span>
<span class="line"><span>             HTTP服务  freedom  remote_addr</span></span></code></pre></div><h2 id="配置系统" tabindex="-1">配置系统 <a class="header-anchor" href="#配置系统" aria-label="Permalink to &quot;配置系统&quot;">​</a></h2><p>采用基于 context 的依赖注入：</p><div class="language-go vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">go</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// 注册配置创建器</span></span>
<span class="line"><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">config.</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">RegisterConfigCreator</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">(</span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">&quot;mysql&quot;</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">, </span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">func</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">(</span><span style="--shiki-light:#E36209;--shiki-dark:#FFAB70;">ctx</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;"> context</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">.</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">Context</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">) </span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">...</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// 从 context 获取配置</span></span>
<span class="line"><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">cfg </span><span style="--shiki-light:#D73A49;--shiki-dark:#F97583;">:=</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;"> config.</span><span style="--shiki-light:#6F42C1;--shiki-dark:#B392F0;">FromContext</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">(ctx, </span><span style="--shiki-light:#032F62;--shiki-dark:#9ECBFF;">&quot;mysql&quot;</span><span style="--shiki-light:#24292E;--shiki-dark:#E1E4E8;">)</span></span></code></pre></div><p>每个包通过 <code>init()</code> 自注册，无需手动导入。</p><h2 id="构建标签系统" tabindex="-1">构建标签系统 <a class="header-anchor" href="#构建标签系统" aria-label="Permalink to &quot;构建标签系统&quot;">​</a></h2><p><code>component/</code> 目录通过 Go build tags 控制功能模块：</p><div class="language-go vp-adaptive-theme"><button title="Copy Code" class="copy"></button><span class="lang">go</span><pre class="shiki shiki-themes github-light github-dark vp-code" tabindex="0"><code><span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// +build full</span></span>
<span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// component/client.go — 全量构建包含客户端</span></span>
<span class="line"></span>
<span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// +build !full,!mini,!client</span></span>
<span class="line"><span style="--shiki-light:#6A737D;--shiki-dark:#6A737D;">// 这个文件不会编译（排除条件）</span></span></code></pre></div><p>这允许构建不同大小的二进制文件，从完整的全功能版本到仅客户端的精简版。</p><h2 id="关键文件索引" tabindex="-1">关键文件索引 <a class="header-anchor" href="#关键文件索引" aria-label="Permalink to &quot;关键文件索引&quot;">​</a></h2><table tabindex="0"><thead><tr><th>功能</th><th>目录</th></tr></thead><tbody><tr><td>代理模式</td><td><code>proxy/</code></td></tr><tr><td>隧道层</td><td><code>tunnel/</code></td></tr><tr><td>API</td><td><code>api/</code></td></tr><tr><td>用户认证</td><td><code>statistic/</code></td></tr><tr><td>路由</td><td><code>tunnel/router/</code></td></tr><tr><td>TLS</td><td><code>tunnel/tls/</code></td></tr><tr><td>WebSocket</td><td><code>tunnel/websocket/</code></td></tr><tr><td>配置</td><td><code>config/</code></td></tr><tr><td>日志</td><td><code>log/</code></td></tr><tr><td>指标</td><td><code>metric/</code></td></tr></tbody></table>`,24)])])}const g=a(e,[["render",i]]);export{k as __pageData,g as default};
