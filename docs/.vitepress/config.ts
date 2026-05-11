import { defineConfig } from 'vitepress'

export default defineConfig({
  title: "l0-git",
  description: "Deterministic project-hygiene quality gates for the open workspace",
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
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
      { text: 'MCP', link: '/mcp/' }
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Introduction',
          items: [
            { text: 'What is l0-git?', link: '/guide/introduction' },
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Configuration', link: '/guide/configuration' }
          ]
        },
        {
          text: 'Integration',
          items: [
            { text: 'VS Code Extension', link: '/guide/vscode' },
            { text: 'Claude Code / MCP', link: '/guide/mcp' }
          ]
        }
      ],
      '/gates/': [
        {
          text: 'Project Hygiene',
          items: [
            { text: 'README Present', link: '/gates/readme-present' },
            { text: 'LICENSE Present', link: '/gates/license-present' },
            { text: 'CONTRIBUTING Present', link: '/gates/contributing-present' },
            { text: 'SECURITY Present', link: '/gates/security-present' },
            { text: 'CHANGELOG Present', link: '/gates/changelog-present' },
            { text: 'CODE_OF_CONDUCT Present', link: '/gates/code-of-conduct-present' }
          ]
        },
        {
          text: 'Git & Repository',
          items: [
            { text: '.gitignore Present', link: '/gates/gitignore-present' },
            { text: 'Gitignore Coverage', link: '/gates/gitignore-coverage' },
            { text: 'Merge Conflict Markers', link: '/gates/merge-conflict-markers' },
            { text: 'Large File Tracked', link: '/gates/large-file-tracked' },
            { text: 'IDE Artifact Tracked', link: '/gates/ide-artifact-tracked' },
            { text: 'Vendored Directory Tracked', link: '/gates/vendored-dir-tracked' },
            { text: 'Unexpected Executable Bit', link: '/gates/unexpected-executable-bit' },
            { text: 'Filename Quality', link: '/gates/filename-quality' }
          ]
        },
        {
          text: 'Security',
          items: [
            { text: 'Secrets Scan', link: '/gates/secrets-scan' },
            { text: 'Connection Strings', link: '/gates/connection-strings' },
            { text: 'Network Scan', link: '/gates/network-scan' },
            { text: 'Secrets Scan History', link: '/gates/secrets-scan-history' }
          ]
        },
        {
          text: 'Quality & Release',
          items: [
            { text: 'Tests Present', link: '/gates/tests-present' },
            { text: 'Version Drift', link: '/gates/version-drift' },
            { text: 'NVMRC Missing', link: '/gates/nvmrc-missing' }
          ]
        },
        {
          text: 'Specialized Lints',
          items: [
            { text: 'Dockerfile Lint', link: '/gates/dockerfile-lint' },
            { text: 'Compose Lint', link: '/gates/compose-lint' },
            { text: 'HTML Lint', link: '/gates/html-lint' },
            { text: 'CSS Lint', link: '/gates/css-lint' },
            { text: 'Markdown Lint', link: '/gates/markdown-lint' }
          ]
        },
        {
          text: 'Governance',
          items: [
            { text: 'CODEOWNERS Present', link: '/gates/codeowners-present' },
            { text: 'Branch Protection', link: '/gates/branch-protection-declared' },
            { text: 'PR Template Present', link: '/gates/pr-template-present' },
            { text: 'Issue Template Present', link: '/gates/issue-template-present' }
          ]
        },
        {
          text: 'Other',
          items: [
            { text: 'Dead Placeholders', link: '/gates/dead-placeholders' },
            { text: 'Uncommented Env Example', link: '/gates/env-example-uncommented' },
            { text: 'Large Blob in History', link: '/gates/large-blob-in-history' }
          ]
        }
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/fabriziosalmi/l0-git' }
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © 2024-present Fabrizio Salmi'
    }
  }
})
