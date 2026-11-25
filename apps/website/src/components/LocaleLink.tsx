import { Link } from '@tanstack/react-router'
import type { MouseEventHandler, ReactNode } from 'react'
import { type Lang, useLang } from '../i18n'

export type SimplePage = 'home' | 'docs' | 'api' | 'playground' | 'showcase' | 'roadmap' | 'tutorial' | 'packages' | 'examples'

export type LocaleTarget =
  | { page: SimplePage }
  | { page: 'doc'; slug: string }
  | { page: 'apiPackage'; splat: string }
  | { page: 'playgroundExample'; example: string }

type CommonProps = {
  lang?: Lang
  children: ReactNode
  className?: string
  hash?: string
  langAttr?: string
  'aria-label'?: string
  activeOptions?: { exact?: boolean }
  activeProps?: { 'aria-current': 'page' }
  onClick?: MouseEventHandler<HTMLAnchorElement>
}

export type LocaleLinkProps = LocaleTarget & CommonProps

// LocaleLink renders a typed TanStack Router Link for either English at the root or a non-default language under /$lang/.
export function LocaleLink(props: LocaleLinkProps) {
  const currentLang = useLang()
  const lang = props.lang ?? currentLang
  const common = {
    className: props.className,
    hash: props.hash,
    lang: props.langAttr,
    'aria-label': props['aria-label'],
    activeOptions: props.activeOptions,
    activeProps: props.activeProps,
    onClick: props.onClick,
  }

  switch (props.page) {
    case 'home':
      return lang === 'en' ? (
        <Link to="/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'docs':
      return lang === 'en' ? (
        <Link to="/docs/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/docs/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'doc':
      return lang === 'en' ? (
        <Link to="/docs/$slug/" params={{ slug: props.slug }} {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/docs/$slug/" params={{ lang, slug: props.slug }} {...common}>
          {props.children}
        </Link>
      )
    case 'api':
      return lang === 'en' ? (
        <Link to="/docs/api/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/docs/api/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'apiPackage':
      return lang === 'en' ? (
        <Link to="/docs/api/$/" params={{ _splat: props.splat }} {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/docs/api/$/" params={{ lang, _splat: props.splat }} {...common}>
          {props.children}
        </Link>
      )
    case 'playground':
      return lang === 'en' ? (
        <Link to="/playground/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/playground/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'playgroundExample':
      return lang === 'en' ? (
        <Link to="/playground/$example/" params={{ example: props.example }} {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/playground/$example/" params={{ lang, example: props.example }} {...common}>
          {props.children}
        </Link>
      )
    case 'showcase':
      return lang === 'en' ? (
        <Link to="/showcase/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/showcase/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'roadmap':
      return lang === 'en' ? (
        <Link to="/roadmap/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/roadmap/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'tutorial':
      return lang === 'en' ? (
        <Link to="/tutorial/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/tutorial/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'packages':
      return lang === 'en' ? (
        <Link to="/packages/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/packages/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
    case 'examples':
      return lang === 'en' ? (
        <Link to="/examples/" {...common}>
          {props.children}
        </Link>
      ) : (
        <Link to="/$lang/examples/" params={{ lang }} {...common}>
          {props.children}
        </Link>
      )
  }
}
