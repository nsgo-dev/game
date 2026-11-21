import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

// A shell command with a copy button, as nsgo.dev's CopyCommand shows one.
export function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command)
    } catch {
      // The command can still be selected.
      return
    }
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className="command">
      <span className="command__prompt" aria-hidden="true">
        $
      </span>
      <code>{command}</code>
      <button type="button" className={copied ? 'command__copy is-copied' : 'command__copy'} aria-label={copied ? 'Copied' : 'Copy'} title="Copy" onClick={copy}>
        <Icon name={copied ? 'check' : 'copy'} size={14} />
      </button>
    </div>
  )
}
