import { defineConfig } from 'vitepress'

export default defineConfig({
  base: '/l0-git/',
  title: "l0-git",
  description: "Deterministic project-hygiene quality gates for the open workspace",
  head: [
    // Everything this site loads is first-party. 'unsafe-inline' is required
    // because VitePress emits an inline appearance script and inline styles.
    // Applied to the built site only: `vitepress dev` serves HMR over a
    // websocket, which a strict connect-src would block as soon as the dev
    // server is not same-origin (--host, or a custom server.hmr.port).
    ...(process.env.NODE_ENV === 'production'
      ? [
          [
            'meta',
            {
              'http-equiv': 'Content-Security-Policy',
              content:
                "default-src 'self'; script-src 'self' 'unsafe-inline'; " +
                "style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
                "font-src 'self'; connect-src 'self'; base-uri 'self'; form-action 'self'",
            },
          ] as [string, Record<string, string>],
        ]
      : []),
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/l0-git/logo.svg' }],
    ['meta', { name: 'theme-color', content: '#000000' }],
    ['meta', { name: 'apple-mobile-web-app-capable', content: 'yes' }],
    ['meta', { name: 'apple-mobile-web-app-status-bar-style', content: 'black' }]
  ],

  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Guide', link: '/guide/introduction' },
      { text: 'Gates', link: '/gates/' },
      { text: 'CLI', link: '/cli/' },
      { text: 'MCP', link: '/guide/mcp' }
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Introduction',
          items: [
            { text: 'What is l0-git?', link: '/guide/introduction' },
            { text: 'Getting started', link: '/guide/getting-started' },
            { text: 'Configuration', link: '/guide/configuration' }
          ]
        },
        {
          text: 'Integration',
          items: [
            { text: 'VS Code extension', link: '/guide/vscode' },
            { text: 'Claude Code / MCP', link: '/guide/mcp' },
            { text: 'CLI reference', link: '/cli/' }
          ]
        }
      ],
      '/gates/': [
        {
          text: 'Project hygiene',
          collapsed: false,
          items: [
            { text: 'README present', link: '/gates/readme-present' },
            { text: 'LICENSE present', link: '/gates/license-present' },
            { text: 'CONTRIBUTING present', link: '/gates/contributing-present' },
            { text: 'SECURITY policy present', link: '/gates/security-present' },
            { text: 'CHANGELOG present', link: '/gates/changelog-present' },
            { text: 'CODE_OF_CONDUCT present', link: '/gates/code-of-conduct-present' },
            { text: 'Pull request template present', link: '/gates/pr-template-present' },
            { text: 'Issue templates present', link: '/gates/issue-template-present' },
            { text: 'CI workflow present', link: '/gates/ci-workflow-present' },
          ]
        },
        {
          text: 'Governance',
          collapsed: false,
          items: [
            { text: 'CODEOWNERS present', link: '/gates/codeowners-present' },
            { text: 'Branch protection declared', link: '/gates/branch-protection-declared' },
          ]
        },
        {
          text: 'Git hygiene',
          collapsed: false,
          items: [
            { text: '.gitignore present', link: '/gates/gitignore-present' },
            { text: '.gitignore coverage', link: '/gates/gitignore-coverage' },
            { text: 'Merge conflict markers', link: '/gates/merge-conflict-markers' },
            { text: 'Large file tracked', link: '/gates/large-file-tracked' },
            { text: 'Vendored directory tracked', link: '/gates/vendored-dir-tracked' },
            { text: 'Editor/IDE artefact tracked', link: '/gates/ide-artifact-tracked' },
            { text: 'Unexpected executable bit', link: '/gates/unexpected-executable-bit' },
            { text: 'File name quality', link: '/gates/filename-quality' },
          ]
        },
        {
          text: 'Security',
          collapsed: false,
          items: [
            { text: 'Secrets scan', link: '/gates/secrets-scan' },
            { text: 'Connection strings', link: '/gates/connection-strings' },
            { text: 'Network scan', link: '/gates/network-scan' },
          ]
        },
        {
          text: 'Git history (opt-in)',
          collapsed: false,
          items: [
            { text: 'Secrets scan (history)', link: '/gates/secrets-scan-history' },
            { text: 'Large blob in history', link: '/gates/large-blob-in-history' },
          ]
        },
        {
          text: 'Containers',
          collapsed: false,
          items: [
            { text: 'Dockerfile lint', link: '/gates/dockerfile-lint' },
            { text: 'Compose lint', link: '/gates/compose-lint' },
          ]
        },
        {
          text: 'Frontend & accessibility',
          collapsed: false,
          items: [
            { text: 'HTML lint', link: '/gates/html-lint' },
            { text: 'CSS lint', link: '/gates/css-lint' },
          ]
        },
        {
          text: 'Documentation',
          collapsed: false,
          items: [
            { text: 'Markdown lint', link: '/gates/markdown-lint' },
            { text: 'Dead placeholders', link: '/gates/dead-placeholders' },
            { text: 'Uncommented .env.example key', link: '/gates/env-example-uncommented' },
          ]
        },
        {
          text: 'Quality & release',
          collapsed: false,
          items: [
            { text: 'Tests present', link: '/gates/tests-present' },
            { text: 'Config parse error', link: '/gates/config-parse-error' },
            { text: 'Version drift', link: '/gates/version-drift' },
            { text: 'Missing .nvmrc / .node-version', link: '/gates/nvmrc-missing' },
          ]
        },
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/fabriziosalmi/l0-git' }
    ],

    footer: {
      message: 
        'Released under the MIT License. · <a href="https://fabriziosalmi.github.io/privacy">Privacy &amp; legal</a>',
      copyright: 'Copyright © 2024-present Fabrizio Salmi'
    }
  }
})
