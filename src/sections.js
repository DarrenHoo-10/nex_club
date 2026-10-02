export const SECTIONS = [
  {
    key: 'tools',
    kind: 'tool',
    label: 'AI工具网站',
    title: '发现真正好用的 AI 工具',
    blurb: '少而精的 AI 工具站点，每一个都值得收藏。',
  },
  {
    key: 'tutorials',
    kind: 'tutorial',
    label: 'AI焚决集合',
    title: '照着做就能成的 AI 秘籍',
    blurb: '一步步的 AI 教程与实战心法，从入门到进阶。',
  },
  {
    key: 'repos',
    kind: 'repo',
    label: 'AI GitHub 项目',
    title: '值得 Star 的 AI 开源项目',
    blurb: '开源社区里真正有价值的 AI 项目。',
  },
]

export const SORT_OPTIONS = [
  { key: 'recommended', label: '推荐', title: '按编辑推荐顺序' },
  { key: 'heat', label: '热度', title: '按热度从高到低' },
  { key: 'latest', label: '最新', title: '按收录时间从新到旧' },
]

export const RELEVANCE_OPTION = { key: 'relevance', label: '相关度', title: '按与关键词的匹配程度' }

const LEGACY = new Set(SECTIONS.map((section) => section.key))

export function sectionByKey(key) {
  return SECTIONS.find((section) => section.key === key) || SECTIONS[0]
}

export function sectionByKind(kind) {
  return SECTIONS.find((section) => section.kind === kind) || SECTIONS[0]
}

export function sectionPath(key, sort) {
  if (!sort) return `/${key}`
  return `/${key}?${new URLSearchParams({ sort }).toString()}`
}

export function legacySection(hash) {
  const key = hash.replace(/^#/, '')
  return LEGACY.has(key) ? key : ''
}
