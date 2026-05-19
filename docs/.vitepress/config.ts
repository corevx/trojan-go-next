import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Trojan-Go-Next Docs',
  description: '使用 Go 实现的完整 Trojan 代理，兼容原版协议与配置格式',
  base: '/trojan-go-next/',

  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      link: '/',
      themeConfig: {
        siteTitle: 'Trojan-Go-Next 文档',
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
        siteTitle: 'Trojan-Go-Next Docs',
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
        { text: '5 分钟快速入门', link: '/guide/quickstart' },
        { text: '安装指南', link: '/guide/install' },
        { text: 'Trojan 原理', link: '/guide/trojan' },
        { text: '配置入门', link: '/guide/config' },
        { text: '完整配置文件', link: '/guide/full-config' },
        { text: '常见问题', link: '/guide/faq' }
      ]
    },
    {
      text: '部署指南',
      collapsed: false,
      items: [
        { text: 'systemd 服务', link: '/deployment/systemd' },
        { text: 'Docker 容器', link: '/deployment/docker' },
        { text: 'TLS 证书管理', link: '/deployment/tls-certificates' },
        { text: '多用户管理', link: '/deployment/multi-user' }
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
        { text: '隧道与反向代理', link: '/features/forward' },
        { text: '透明代理', link: '/features/nat' },
        { text: '可插拔传输层', link: '/features/plugin' },
        { text: 'SNI 中继方案', link: '/features/nginx-relay' },
        { text: '自定义协议栈', link: '/features/custom-stack' },
        { text: 'URL 分享链接', link: '/features/url-scheme' }
      ]
    },
    {
      text: 'API 与管理',
      collapsed: false,
      items: [
        { text: 'gRPC API', link: '/management/grpc-api' },
        { text: 'REST API（v0.11.0）', link: '/management/rest-api' },
        { text: '健康检查与监控（v0.11.0）', link: '/management/monitor' },
        { text: 'Prometheus 指标', link: '/management/metrics' }
      ]
    },
    {
      text: '开发指南',
      collapsed: true,
      items: [
        { text: '开发者入门', link: '/developer/index' },
        { text: '架构概览', link: '/developer/overview' },
        { text: '架构设计', link: '/developer/architecture' },
        { text: '编译与构建', link: '/developer/build' },
        { text: '隧道 API 参考', link: '/developer/tunnel-api' },
        { text: 'Trojan 协议', link: '/developer/trojan-protocol' },
        { text: 'WebSocket 实现', link: '/developer/websocket' },
        { text: '多路复用实现', link: '/developer/mux' },
        { text: 'SimpleSocks 协议', link: '/developer/simplesocks' },
        { text: '传输层插件开发', link: '/developer/plugin-dev' },
        { text: 'API 开发', link: '/developer/api-dev' },
        { text: 'URL 方案规范', link: '/developer/url-spec' },
        { text: '结构化日志（v0.11.0）', link: '/developer/structured-logging' },
        { text: '指标系统（v0.11.0）', link: '/developer/metrics-impl' },
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
        { text: '5 分钟入门', link: '/guide/quickstart' },
        { text: '安装指南', link: '/guide/install' },
        { text: '配置入门', link: '/guide/config' },
        { text: '完整配置文件', link: '/guide/full-config' },
        { text: '常见问题', link: '/guide/faq' }
      ]
    },
    {
      text: '功能',
      items: [
        { text: '多路复用', link: '/features/mux' },
        { text: 'WebSocket CDN', link: '/features/websocket' },
        { text: '路由分流', link: '/features/router' },
        { text: 'AEAD 加密', link: '/features/aead' },
        { text: 'REST API', link: '/management/rest-api' },
        { text: '监控', link: '/management/monitor' }
      ]
    },
    {
      text: '部署',
      items: [
        { text: 'systemd', link: '/deployment/systemd' },
        { text: 'Docker', link: '/deployment/docker' },
        { text: 'TLS 证书', link: '/deployment/tls-certificates' },
        { text: '多用户', link: '/deployment/multi-user' }
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
        { text: 'Overview', link: '/en/features/' }
      ]
    },
    {
      text: 'Developer',
      collapsed: false,
      items: [
        { text: 'Overview', link: '/en/developer/' }
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
