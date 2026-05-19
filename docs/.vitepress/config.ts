import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Trojan-Go Docs',
  description: '使用 Go 实现的完整 Trojan 代理，兼容原版协议与配置格式',
  base: '/trojan-go-next/',

  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      link: '/',
      themeConfig: {
        siteTitle: 'Trojan-Go 文档',
        nav: createNavZH(),
        sidebar: createSidebarZH(),
        editLink: {
          pattern: 'https://github.com/corevx/trojan-go-next/edit/main/docs/:path',
          text: '在 GitHub 上编辑此页'
        },
        docFooter: { prev: '上一篇', next: '下一篇' },
        outline: { label: '本页目录' },
        lastUpdated: { text: '最后更新于' },
        returnToTopLabel: '回到顶部',
        sidebarMenuLabel: '菜单',
        darkModeSwitchLabel: '主题',
        lightModeSwitchTitle: '浅色',
        darkModeSwitchTitle: '深色'
      }
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      themeConfig: {
        siteTitle: 'Trojan-Go Docs',
        nav: createNavEN(),
        sidebar: createSidebarEN(),
        editLink: {
          pattern: 'https://github.com/corevx/trojan-go-next/edit/main/docs/:path',
          text: 'Edit this page on GitHub'
        }
      }
    }
  },

  themeConfig: {
    socialLinks: [
      { icon: 'github', link: 'https://github.com/corevx/trojan-go-next' }
    ],
    search: {
      provider: 'local'
    }
  },

  lastUpdated: true
})

function createSidebarZH() {
  return [
    {
      text: '快速开始',
      collapsed: false,
      items: [
        { text: '安装指南', link: '/guide/install' },
        { text: 'Trojan 原理入门', link: '/guide/trojan' },
        { text: '配置入门', link: '/guide/config' },
        { text: '完整配置文件', link: '/guide/full-config' },
        { text: '常见问题', link: '/guide/faq' }
      ]
    },
    {
      text: '功能特性',
      collapsed: false,
      items: [
        { text: '多路复用', link: '/features/mux' },
        { text: 'WebSocket CDN 中转', link: '/features/websocket' },
        { text: '路由分流与广告屏蔽', link: '/features/router' },
        { text: 'AEAD 二次加密', link: '/features/aead' },
        { text: '可插拔传输层', link: '/features/plugin' },
        { text: '隧道与反向代理', link: '/features/forward' },
        { text: '透明代理', link: '/features/nat' },
        { text: 'SNI 中继方案', link: '/features/nginx-relay' },
        { text: '自定义协议栈', link: '/features/customize-protocol-stack' }
      ]
    },
    {
      text: 'API 与管理',
      collapsed: false,
      items: [
        { text: 'gRPC API', link: '/api/grpc' },
        { text: 'REST API（v0.11.0 新增）', link: '/features/rest-api' },
        { text: '健康检查与监控（v0.11.0 新增）', link: '/features/monitor' }
      ]
    },
    {
      text: '开发指南',
      collapsed: true,
      items: [
        { text: '架构概览', link: '/developer/overview' },
        { text: '编译与构建', link: '/developer/build' },
        { text: 'Trojan 协议', link: '/developer/trojan' },
        { text: 'WebSocket', link: '/developer/websocket' },
        { text: '多路复用', link: '/developer/mux' },
        { text: 'SimpleSocks 协议', link: '/developer/simplesocks' },
        { text: '传输层插件开发', link: '/developer/plugin' },
        { text: 'API 开发', link: '/developer/api' },
        { text: 'URL 方案（草案）', link: '/developer/url' },
        { text: '结构化日志（v0.11.0 新增）', link: '/developer/structured-logging' },
        { text: '指标系统（v0.11.0 新增）', link: '/developer/metrics' },
        { text: '更新日志', link: '/developer/changelog' }
      ]
    }
  ]
}

function createNavZH() {
  return [
    { text: '首页', link: '/' },
    {
      text: '快速开始',
      items: [
        { text: '安装指南', link: '/guide/install' },
        { text: '配置入门', link: '/guide/config' },
        { text: '完整配置文件', link: '/guide/full-config' }
      ]
    },
    {
      text: '功能',
      items: [
        { text: '多路复用', link: '/features/mux' },
        { text: 'WebSocket CDN', link: '/features/websocket' },
        { text: '路由分流', link: '/features/router' },
        { text: 'AEAD 加密', link: '/features/aead' },
        { text: 'REST API', link: '/features/rest-api' },
        { text: '监控', link: '/features/monitor' }
      ]
    },
    {
      text: '开发',
      items: [
        { text: '架构概览', link: '/developer/overview' },
        { text: '编译构建', link: '/developer/build' },
        { text: '更新日志', link: '/developer/changelog' }
      ]
    },
    { text: 'GitHub', link: 'https://github.com/corevx/trojan-go-next' }
  ]
}

function createSidebarEN() {
  return [
    {
      text: 'Getting Started',
      collapsed: false,
      items: [
        { text: 'Installation', link: '/en/guide/install' },
        { text: 'Configuration', link: '/en/guide/config' }
      ]
    },
    {
      text: 'Features',
      collapsed: false,
      items: [
        { text: 'Multiplexing', link: '/en/features/mux' },
        { text: 'WebSocket CDN', link: '/en/features/websocket' },
        { text: 'Routing', link: '/en/features/router' },
        { text: 'AEAD Encryption', link: '/en/features/aead' },
        { text: 'REST API (v0.11.0)', link: '/en/features/rest-api' },
        { text: 'Monitoring (v0.11.0)', link: '/en/features/monitor' }
      ]
    }
  ]
}

function createNavEN() {
  return [
    { text: 'Home', link: '/en/' },
    {
      text: 'Getting Started',
      items: [
        { text: 'Installation', link: '/en/guide/install' },
        { text: 'Configuration', link: '/en/guide/config' }
      ]
    },
    { text: 'GitHub', link: 'https://github.com/corevx/trojan-go-next' }
  ]
}
