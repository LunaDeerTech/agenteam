export function optionKeys(event: KeyboardEvent) {
  const root = event.currentTarget as HTMLElement
  const items = Array.from(root.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'))
  if (!items.length) return
  const current = items.indexOf(document.activeElement as HTMLButtonElement)
  const direction = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
  let index: number | undefined
  if (direction) index = (current + direction + items.length) % items.length
  if (event.key === 'Home') index = 0
  if (event.key === 'End') index = items.length - 1
  if (index !== undefined) {
    event.preventDefault()
    items[index]?.focus()
  } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && event.key !== ' ') {
    const match = items
      .slice(current + 1)
      .concat(items.slice(0, current + 1))
      .find((el) =>
        el.textContent?.trim().toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()),
      )
    if (match) {
      event.preventDefault()
      match.focus()
    }
  }
}
