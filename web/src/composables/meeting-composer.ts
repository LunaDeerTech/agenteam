import type { MeetingNode, MeetingReference } from '../api/meetings'

export type MeetingMentionChoice = Readonly<{
  node: Extract<MeetingNode, { type: 'mention' }>
  label: string
  kind: 'agent' | 'user'
}>
export const meetingNodeLength = (node: MeetingNode) =>
  node.type === 'text' ? node.text.length : 1
export const meetingDraftLength = (nodes: readonly MeetingNode[]) =>
  nodes.reduce((length, node) => length + meetingNodeLength(node), 0)

export function normalizeMeetingDraft(nodes: readonly MeetingNode[]): MeetingNode[] {
  const result: MeetingNode[] = []
  for (const node of nodes) {
    if (node.type !== 'text') {
      result.push(
        node.type === 'mention' ? { ...node } : { ...node, reference: { ...node.reference } },
      )
      continue
    }
    if (!node.text) continue
    const last = result.at(-1)
    if (last?.type === 'text')
      result[result.length - 1] = { type: 'text', text: last.text + node.text }
    else result.push({ ...node })
  }
  return result.length ? result : [{ type: 'text', text: '' }]
}

// Text offsets use the DOM's UTF-16 positions; every captured identity is one
// atomic position. Display labels are never parsed back into an identity.
export function replaceMeetingRange(
  nodes: readonly MeetingNode[],
  start: number,
  end: number,
  insertion: readonly MeetingNode[],
) {
  const length = meetingDraftLength(nodes)
  if (
    !Number.isInteger(start) ||
    !Number.isInteger(end) ||
    start < 0 ||
    end < start ||
    end > length
  )
    throw new Error('INVALID_MEETING_SELECTION')
  const before: MeetingNode[] = [],
    after: MeetingNode[] = []
  let offset = 0
  for (const node of nodes) {
    const next = offset + meetingNodeLength(node)
    if (next <= start) before.push(node)
    else if (node.type === 'text' && start > offset)
      before.push({ type: 'text', text: node.text.slice(0, start - offset) })
    if (offset >= end) after.push(node)
    else if (node.type === 'text' && next > end)
      after.push({ type: 'text', text: node.text.slice(end - offset) })
    offset = next
  }
  return normalizeMeetingDraft([...before, ...insertion, ...after])
}

// Match the server's minimal scheme/host/default-port/empty-path normalization;
// preserve the path, query and fragment, including meaningful dot segments.
export function meetingLink(value: string, label = ''): MeetingReference | null {
  if (
    !value ||
    value.trim() !== value ||
    /[\u0000-\u0020\u007f\\]/.test(value) ||
    /%(?![0-9a-f]{2})/i.test(value) ||
    new TextEncoder().encode(value).length > 8192
  )
    return null
  const match = /^(https?):\/\/([^/?#]+)([^?#]*)([?#].*)?$/i.exec(value)
  if (!match || match[2]!.includes('@')) return null
  try {
    const parsed = new URL(value)
    if (!parsed.hostname || parsed.username || parsed.password) return null
    const authority = /^(\[[^\]]+\]|[^:]+)(?::([0-9]+))?$/.exec(match[2]!)
    if (!authority) return null
    let port = authority[2] ?? ''
    if (port && (Number(port) < 1 || Number(port) > 65535)) return null
    const scheme = match[1]!.toLowerCase()
    if ((scheme === 'http' && port === '80') || (scheme === 'https' && port === '443')) port = ''
    const id =
      scheme +
      '://' +
      authority[1]!.toLowerCase() +
      (port ? ':' + port : '') +
      (match[3] || '/') +
      (match[4] || '')
    return { kind: 'link', id, ...(label ? { label } : {}) }
  } catch {
    return null
  }
}
export function meetingReferenceKey(reference: MeetingReference) {
  return (
    reference.kind +
    ':' +
    (reference.kind === 'link' ? (meetingLink(reference.id)?.id ?? reference.id) : reference.id)
  )
}
export function sameMeetingMention(a: MeetingNode, b: MeetingNode) {
  return (
    a.type === 'mention' &&
    b.type === 'mention' &&
    a.agent_id === b.agent_id &&
    a.participant_id === b.participant_id
  )
}

// Only nodes created by this editor's private map retain a typed identity.
// Browser edits, HTML paste and arbitrary data attributes cannot mint a chip.
export function readMeetingEditable(
  root: HTMLElement,
  chips: WeakMap<Node, MeetingNode>,
): MeetingNode[] {
  const result: MeetingNode[] = []
  const visit = (node: Node) => {
    const known = chips.get(node)
    if (known) {
      result.push(known)
      return
    }
    if (node.nodeType === Node.TEXT_NODE) {
      result.push({ type: 'text', text: node.textContent ?? '' })
      return
    }
    if (node instanceof HTMLBRElement) {
      result.push({ type: 'text', text: '\n' })
      return
    }
    for (const child of node.childNodes) visit(child)
  }
  for (const node of root.childNodes) visit(node)
  return normalizeMeetingDraft(result)
}
export function meetingSelection(
  root: HTMLElement,
  chips: WeakMap<Node, MeetingNode>,
): { start: number; end: number } | null {
  const selection = root.ownerDocument.getSelection()
  if (
    !selection?.rangeCount ||
    !root.contains(selection.anchorNode) ||
    !root.contains(selection.focusNode)
  )
    return null
  function position(target: Node | null, index: number) {
    let offset = 0,
      found = false
    const size = (node: Node): number =>
      chips.has(node)
        ? 1
        : node.nodeType === Node.TEXT_NODE
          ? (node.textContent?.length ?? 0)
          : node instanceof HTMLBRElement
            ? 1
            : [...node.childNodes].reduce((n, child) => n + size(child), 0)
    const visit = (node: Node) => {
      if (node === target) {
        offset +=
          node.nodeType === Node.TEXT_NODE
            ? index
            : [...node.childNodes].slice(0, index).reduce((n, child) => n + size(child), 0)
        found = true
      } else if (
        chips.has(node) ||
        node.nodeType === Node.TEXT_NODE ||
        node instanceof HTMLBRElement
      )
        offset += size(node)
      else
        for (const child of node.childNodes) {
          visit(child)
          if (found) break
        }
    }
    visit(root)
    return found ? offset : null
  }
  const a = position(selection.anchorNode, selection.anchorOffset),
    b = position(selection.focusNode, selection.focusOffset)
  return a === null || b === null ? null : { start: Math.min(a, b), end: Math.max(a, b) }
}
export function focusMeetingEditable(
  root: HTMLElement,
  chips: WeakMap<Node, MeetingNode>,
  offset: number,
) {
  root.focus()
  const range = root.ownerDocument.createRange()
  let remaining = offset,
    selected = false
  for (const [index, node] of [...root.childNodes].entries()) {
    if (remaining === 0) {
      range.setStart(root, index)
      selected = true
      break
    }
    if (chips.has(node)) {
      if (remaining === 0) {
        range.setStart(root, index)
        selected = true
        break
      }
      remaining--
    } else if (node.nodeType === Node.TEXT_NODE) {
      const length = node.textContent?.length ?? 0
      if (remaining <= length) {
        range.setStart(node, remaining)
        selected = true
        break
      }
      remaining -= length
    }
  }
  if (!selected) range.setStart(root, root.childNodes.length)
  range.collapse(true)
  const selection = root.ownerDocument.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}
