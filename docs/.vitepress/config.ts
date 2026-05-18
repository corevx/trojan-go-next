import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'zh-CN',
  title: 'Trojan-Go Docs',
  description: '使用 Go 实现的完整 Trojan 代理，兼容原版协议与配置格式',
  base: '/trojan-go-next/',

  themeConfig: {
    siteTitle: 'Trojan-Go 文档',

    nav: [
      { text: '首页', link: '/' },
      { text: 'GitHub', link: 'https://github.com/corevx/trojan-go-next' }
    ],

    search: {
      provider: 'local'
    },

    sidebar: {
      '/basic/': [
        {
          text: '基本配置',
          items: [
            { text: '简介', link: '/basic/' },
            { text: 'Trojan 基本原理', link: '/basic/trojan' },
            { text: '正确配置 Trojan-Go', link: '/basic/config' },
            { text: '完整的配置文件', link: '/basic/full-config' }
          ]
        }
      ],
      '/advance/': [
        {
          text: '高级配置',
          collapsed: false,
          items: [
            { text: '简介', link: '/advance/' },
            { text: '启用多路复用提升网络并发性能', link: '/advance/mux' },
            { text: '使用 WebSocket 进行 CDN 转发和抵抗中间人攻击', link: '/advance/websocket' },
            { text: '国内直连和广告屏蔽', link: '/advance/router' },
            { text: '使用 Shadowsocks AEAD 进行二次加密', link: '/advance/aead' },
            { text: '隧道和反向代理', link: '/advance/forward' },
            { text: '透明代理', link: '/advance/nat' },
            { text: '基于 SNI 代理的多路径分流中继方案', link: '/advance/nginx-relay' },
            { text: '使用 Shadowsocks 插件/可插拔传输层', link: '/advance/plugin' },
            { text: '自定义协议栈', link: '/advance/customize-protocol-stack' },
            { text: '使用 API 动态管理用户', link: '/advance/api' },
            { text: 'REST API（v0.11.0 新增）', link: '/advance/rest-api' },
            { text: '健康检查与监控（v0.11.0 新增）', link: '/advance/monitor' }
          ]
        }
      ],
      '/developer/': [
        {
          text: '实现细节和开发指南',
          collapsed: false,
          items: [
            { text: '简介', link: '/developer/' },
            { text: '基本介绍', link: '/developer/overview' },
            { text: '编译和自定义 Trojan-Go', link: '/developer/build' },
            { text: 'Trojan 协议', link: '/developer/trojan' },
            { text: 'API 开发', link: '/developer/api' },
            { text: 'WebSocket', link: '/developer/websocket' },
            { text: '多路复用', link: '/developer/mux' },
            { text: 'SimpleSocks 协议', link: '/developer/simplesocks' },
            { text: '可插拔传输层插件开发', link: '/developer/plugin' },
            { text: 'URL 方案（草案）', link: '/developer/url' },
            { text: '结构化日志（v0.11.0 新增）', link: '/developer/structured-logging' },
            { text: '指标系统（v0.11.0 新增）', link: '/developer/metrics' },
            { text: '更新日志', link: '/developer/changelog' }
          ]
        }
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/corevx/trojan-go-next' }
    ],

    editLink: {
      pattern: 'https://github.com/corevx/trojan-go-next/edit/main/docs/:path',
      text: '在 GitHub 上编辑此页'
    }
  },

  lastUpdated: true
})
