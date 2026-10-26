// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    {
      type: 'doc',
      id: 'getting-started',
      label: 'Getting Started',
    },
    {
      type: 'doc',
      id: 'whats-new',
      label: 'What\'s New',
    },
    {
      type: 'doc',
      id: 'faq',
      label: 'FAQ',
    },
    {
      type: 'doc',
      id: 'architecture',
      label: 'Architecture',
    },
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/budget-windows',
        'concepts/session-budgets',
        'concepts/agent-loop-detection',
        'concepts/context-acceleration',
        'concepts/security-events',
        'concepts/concurrency-correctness',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      items: [
        'guides/policy-engine',
        'guides/framework-adapters',
        'guides/mcp-setup',
        'guides/agent-cli-integrations',
        'guides/ci-cd-integration',
        'guides/kubernetes-helm',
        'guides/monitoring-grafana',
      ],
    },
    {
      type: 'category',
      label: 'SDKs',
      items: [
        'sdks/python',
        'sdks/typescript',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/cli',
        'reference/headers',
        'reference/config',
        'reference/release-checklist',
      ],
    },
    {
      type: 'category',
      label: 'Contributing',
      items: [
        'contributing/guide',
        'contributing/adding-a-provider',
      ],
    },
    {
      type: 'category',
      label: 'Security & Governance',
      collapsed: false,
      items: [
        'security/security-model',
        'security/owasp-mapping',
        'security/policy',
      ],
    },
  ],
};

export default sidebars;
