import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

const githubAttachmentUrl = /^https:\/\/github\.com\/user-attachments\/assets\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

function safeUrl(value) {
  const url = value.trim()
  if (/^(https?:|mailto:|#|\/)/i.test(url) || (!/^[a-z][a-z0-9+.-]*:/i.test(url) && !url.startsWith('//'))) return url
  return '#'
}

const relativeUrl = (value) => !/^(?:[a-z][a-z0-9+.-]*:|\/|#)/i.test(value)
const assetType = (path) => ({ png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', svg: 'image/svg+xml' })[(path.split('.').pop() || '').toLowerCase()]

function decodeHtmlAttribute(value) {
  return value.replace(/&(?:#(\d+)|#x([0-9a-f]+)|(amp|quot|apos|lt|gt));/gi, (entity, decimal, hexadecimal, named) => {
    if (decimal || hexadecimal) {
      const codePoint = decimal ? Number(decimal) : parseInt(hexadecimal, 16)
      return codePoint <= 0x10ffff && !(codePoint >= 0xd800 && codePoint <= 0xdfff) ? String.fromCodePoint(codePoint) : '\ufffd'
    }
    return ({ amp: '&', quot: '"', apos: "'", lt: '<', gt: '>' })[named.toLowerCase()]
  })
}

function githubImageNode(value) {
  const tag = value.trim().match(/^<img\b([\s\S]*?)\/?\s*>$/i)
  if (!tag) return null

  const attributes = {}
  let source = tag[1]
  while (source.trim()) {
    const attribute = source.match(/^\s*([a-z][\w:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')/i)
    if (!attribute) return null
    const name = attribute[1].toLowerCase()
    if (!['src', 'alt', 'width', 'height'].includes(name) || name in attributes) return null
    attributes[name] = decodeHtmlAttribute(attribute[2] ?? attribute[3])
    source = source.slice(attribute[0].length)
  }

  if (!githubAttachmentUrl.test(attributes.src || '')) return null
  const dimensions = {}
  for (const name of ['width', 'height']) {
    if (attributes[name] && /^\d{1,5}$/.test(attributes[name]) && Number(attributes[name]) > 0) dimensions[name] = Number(attributes[name])
  }
  return {
    type: 'image',
    url: attributes.src,
    alt: attributes.alt || '',
    title: null,
    data: { hProperties: dimensions },
  }
}

function remarkGithubImages() {
  return (tree) => {
    const visit = (node) => {
      if (!node.children) return
      node.children = node.children.map((child) => {
        if (child.type === 'html') return githubImageNode(child.value) || child
        visit(child)
        return child
      })
    }
    visit(tree)
  }
}

function MarkdownImage({ src, alt, loadAsset, width, height }) {
  const [resolved, setResolved] = useState(relativeUrl(src) && loadAsset ? '' : safeUrl(src))
  useEffect(() => {
    let active = true
    if (!relativeUrl(src) || !loadAsset) { setResolved(safeUrl(src)); return () => { active = false } }
    loadAsset(src).then((file) => {
      if (active) setResolved(`data:${assetType(src) || 'application/octet-stream'};base64,${file.content}`)
    }).catch(() => active && setResolved('#'))
    return () => { active = false }
  }, [src, loadAsset])
  if (!resolved) return <span className="markdown-image-loading">{alt}</span>
  if (resolved === '#') return alt
  return <img src={resolved} alt={alt} width={width} height={height} loading="lazy" />
}

export default function Markdown({ children, className = '', loadAsset, onOpenPath }) {
  return <div className={`markdown-body ${className}`.trim()}>
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkGithubImages]}
      urlTransform={safeUrl}
      components={{
        a: ({ node: _node, href = '', children: content, ...props }) => {
          const repositoryPath = relativeUrl(href) && onOpenPath
          const external = /^https?:/i.test(href)
          return <a
            {...props}
            href={href}
            target={external ? '_blank' : undefined}
            rel={external ? 'noreferrer' : undefined}
            onClick={repositoryPath ? (event) => { event.preventDefault(); onOpenPath(href) } : undefined}
          >{content}</a>
        },
        img: ({ node: _node, src = '', alt = '', width, height }) => <MarkdownImage src={src} alt={alt} width={width} height={height} loadAsset={loadAsset} />,
      }}
    >{String(children || '')}</ReactMarkdown>
  </div>
}
