import { useState } from 'react'
import { cn } from '#/lib/utils'

export function ExternalLinkButton({
  href,
  name,
  short,
  iconSrc,
  iconClassName,
}: {
  href: string
  name: string
  short: string
  iconSrc?: string
  iconClassName?: string
}) {
  const [iconFailed, setIconFailed] = useState(false)
  const showIcon = iconSrc && !iconFailed

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={name}
      className={cn(
        'flex size-10 items-center justify-center rounded-full border border-border bg-background/80',
        'text-caption transition hover:bg-muted hover:text-foreground',
      )}
    >
      {showIcon ? (
        <img
          src={iconSrc}
          alt=""
          className={cn('size-5 object-contain', iconClassName)}
          onError={() => setIconFailed(true)}
        />
      ) : (
        <span className="font-semibold text-xs">{short}</span>
      )}
    </a>
  )
}
