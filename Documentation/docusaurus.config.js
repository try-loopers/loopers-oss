// @ts-check
import {themes as prismThemes} from 'prism-react-renderer';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Loopers',
  tagline: 'Break the loop before it breaks your budget.',
  favicon: 'img/icon.svg',

  future: { v4: true },

  url: 'https://docs.tryloopers.com',
  baseUrl: '/',

  organizationName: 'try-loopers',
  projectName: 'loopers-oss',

  onBrokenLinks: 'warn',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  headTags: [
    {
      tagName: 'link',
      attributes: {
        rel: 'alternate',
        type: 'text/plain',
        title: 'LLM Text Representation',
        href: '/llms.txt',
      },
    },
    {
      tagName: 'script',
      attributes: {
        type: 'application/ld+json',
      },
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@type': 'SoftwareApplication',
        name: 'Loopers',
        applicationCategory: 'SecurityApplication',
        operatingSystem: 'Linux, macOS, Windows',
        offers: {
          '@type': 'Offer',
          price: '0',
          priceCurrency: 'USD',
        },
        description: 'Baremetal, zero-delay firewall for AI agents and LLMs. Intercepts requests across 500+ AI models, stops runaway agent loops, and enforces MCP tool budgets with sub-millisecond overhead.',
        url: 'https://docs.tryloopers.com',
        sameAs: [
          'https://github.com/try-loopers/loopers-oss',
          'https://tryloopers.com',
        ],
        license: 'https://opensource.org/licenses/MIT',
      }),
    },
    {
      tagName: 'script',
      attributes: {
        type: 'application/ld+json',
      },
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@type': 'Organization',
        name: 'Loopers',
        url: 'https://tryloopers.com',
        logo: 'https://docs.tryloopers.com/img/icon.svg',
        sameAs: ['https://github.com/try-loopers/loopers-oss'],
      }),
    },
  ],

  plugins: ['@docusaurus/plugin-vercel-analytics'],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          editUrl: 'https://github.com/try-loopers/loopers-oss/edit/main/Documentation/',
        },
        blog: {
          showReadingTime: true,
          feedOptions: { type: ['rss', 'atom'], xslt: true },
          editUrl: 'https://github.com/try-loopers/loopers-oss/edit/main/Documentation/',
          onInlineTags: 'warn',
          onInlineAuthors: 'warn',
          onUntruncatedBlogPosts: 'warn',
        },
        sitemap: {
          lastmod: 'date',
          changefreq: 'weekly',
          priority: 0.5,
          ignorePatterns: ['/tags/**'],
          filename: 'sitemap.xml',
        },
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/loopers-social-card.png',
      metadata: [
        {
          name: 'keywords',
          content:
            'AI firewall, agent loop breaker, LLMjacking prevention, MCP governance, Model Context Protocol proxy, LiteLLM alternative, Bifrost alternative, token rate limiter, cost control, prompt security, LLM proxy',
        },
        {
          name: 'description',
          content:
            'Loopers is a high-performance open-source AI firewall and proxy for autonomous agents and LLMs. Stop runaway loops, enforce MCP tool budgets, and prevent token overspending with zero latency penalty.',
        },
        { name: 'author', content: 'Loopers Team' },
        { name: 'twitter:card', content: 'summary_large_image' },
        { name: 'twitter:title', content: 'Loopers – The Firewall for the Agentic Era' },
        {
          name: 'twitter:description',
          content:
            'Zero-delay firewall for autonomous agents & LLMs. Intercepts traffic across 500+ AI models, stops infinite loops, and governs MCP tool calls.',
        },
      ],
      colorMode: {
        defaultMode: 'light',
        disableSwitch: false,
        respectPrefersColorScheme: false,
      },
      navbar: {
        title: 'Loopers',
        logo: {
          src: 'img/transparent.svg',
          href: 'https://tryloopers.com',
          target: '_self',
        },
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'docsSidebar',
            position: 'left',
            label: 'Open Source',
          },
          {
            type: 'docSidebar',
            sidebarId: 'enterpriseSidebar',
            position: 'left',
            label: 'Enterprise In-VPC',
          },
          { to: '/docs/faq', label: 'FAQ', position: 'left' },
          { to: '/docs/reference/cli', label: 'CLI Reference', position: 'left' },
          { to: '/docs/architecture', label: 'Architecture', position: 'left' },
          {
            href: 'https://github.com/try-loopers/loopers-oss',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Documentation',
            items: [
              { label: 'Getting Started', to: '/docs/getting-started' },
              { label: 'Architecture', to: '/docs/architecture' },
              { label: 'FAQ', to: '/docs/faq' },
              { label: 'CLI Reference', to: '/docs/reference/cli' },
              { label: 'API Headers', to: '/docs/reference/headers' },
            ],
          },
          {
            title: 'Concepts',
            items: [
              { label: 'Budget Windows', to: '/docs/concepts/budget-windows' },
              { label: 'Session Budgets', to: '/docs/concepts/session-budgets' },
              { label: 'Concurrency & Correctness', to: '/docs/concepts/concurrency-correctness' },
            ],
          },
          {
            title: 'SDKs',
            items: [
              { label: 'Python SDK', to: '/docs/sdks/python' },
              { label: 'TypeScript SDK', to: '/docs/sdks/typescript' },
            ],
          },
          {
            title: 'Community & LLMs',
            items: [
              { label: 'GitHub', href: 'https://github.com/try-loopers/loopers-oss' },
              { label: 'Loopers Cloud', href: 'https://tryloopers.com' },
              { label: 'llms.txt', href: 'https://docs.tryloopers.com/llms.txt' },
              { label: 'llms-full.txt', href: 'https://docs.tryloopers.com/llms-full.txt' },
              { label: 'Contributing', to: '/docs/contributing/guide' },
              { label: 'Security Policy', to: '/docs/security/security-model' },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} Loopers. MIT License.`,
      },
      prism: {
        theme: prismThemes.vsDark,
        darkTheme: prismThemes.vsDark,
        additionalLanguages: ['bash', 'yaml', 'lua', 'go', 'python', 'typescript'],
      },
    }),
};

export default config;
