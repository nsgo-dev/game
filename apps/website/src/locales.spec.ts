import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { localizeDocLinks } from '../plugins/content.ts'
import { SHOWCASE_ENTRIES } from './content/showcase.ts'
import { copies } from './copy.ts'
import { nsgoUrl } from './links.ts'
import { htmlLang, isLang, isNonDefaultLang, LANGS, langNames, matchBrowserLang, NON_DEFAULT_LANGS, splitLocalePath } from './locales.ts'

describe('locales', () => {
  test('matchBrowserLang resolves all supported languages and regional variants', () => {
    assert.equal(matchBrowserLang(['en-US', 'zh-CN']), 'en')
    assert.equal(matchBrowserLang(['zh-CN', 'en']), 'zh')
    assert.equal(matchBrowserLang(['zh-SG']), 'zh')
    assert.equal(matchBrowserLang(['zh-Hans']), 'zh')
    assert.equal(matchBrowserLang(['zh-Hans-HK']), 'zh')
    assert.equal(matchBrowserLang(['zh']), 'zh')
    assert.equal(matchBrowserLang(['zh-TW']), 'zh-tw')
    assert.equal(matchBrowserLang(['zh-HK']), 'zh-tw')
    assert.equal(matchBrowserLang(['zh-MO']), 'zh-tw')
    assert.equal(matchBrowserLang(['zh-Hant']), 'zh-tw')
    assert.equal(matchBrowserLang(['zh_TW']), 'zh-tw')
    assert.equal(matchBrowserLang(['ja-JP']), 'ja')
    assert.equal(matchBrowserLang(['ko-KR']), 'ko')
    assert.equal(matchBrowserLang(['es-MX']), 'es')
    assert.equal(matchBrowserLang(['fr-CA']), 'fr')
    assert.equal(matchBrowserLang(['de-AT']), 'de')
    assert.equal(matchBrowserLang(['pt-BR']), 'pt')
    assert.equal(matchBrowserLang(['pt_PT']), 'pt')
    assert.equal(matchBrowserLang(['ru-RU']), 'ru')
    assert.equal(matchBrowserLang(['nl-NL', 'de-DE']), 'de')
    assert.equal(matchBrowserLang(['sv-SE', 'nb-NO']), 'en')
    assert.equal(matchBrowserLang([]), 'en')
    assert.equal(matchBrowserLang(['', null, undefined]), 'en')
  })

  test('splitLocalePath separates locale prefix and subpath for root and sub-path bases', () => {
    assert.deepEqual(splitLocalePath('/', '/'), { lang: 'en', path: '' })
    assert.deepEqual(splitLocalePath('/docs/loop/', '/'), { lang: 'en', path: 'docs/loop/' })
    assert.deepEqual(splitLocalePath('/zh/', '/'), { lang: 'zh', path: '' })
    assert.deepEqual(splitLocalePath('/zh', '/'), { lang: 'zh', path: '' })
    assert.deepEqual(splitLocalePath('/zh-tw/docs/loop/', '/'), { lang: 'zh-tw', path: 'docs/loop/' })
    assert.deepEqual(splitLocalePath('/ja/showcase/', '/'), { lang: 'ja', path: 'showcase/' })
    assert.deepEqual(splitLocalePath('/game/ko/playground/caverun/', '/game/'), { lang: 'ko', path: 'playground/caverun/' })
    assert.deepEqual(splitLocalePath('/game/docs/', '/game/'), { lang: 'en', path: 'docs/' })
    assert.deepEqual(splitLocalePath('/unknown/path/', '/'), { lang: 'en', path: 'unknown/path/' })
  })

  test('guards and metadata cover every declared language', () => {
    assert.equal(LANGS.length, 10)
    assert.equal(NON_DEFAULT_LANGS.length, 9)
    for (const lang of LANGS) {
      assert.ok(isLang(lang))
      assert.equal(isNonDefaultLang(lang), lang !== 'en')
      assert.ok(langNames[lang].length > 0)
      assert.ok(htmlLang(lang).length > 0)
    }
    assert.equal(isLang('xx'), false)
    assert.equal(isNonDefaultLang('en'), false)
  })

  test('nsgoUrl only adds /zh/ for Simplified Chinese', () => {
    assert.equal(nsgoUrl('en', 'docs/'), 'https://nsgo.dev/docs/')
    assert.equal(nsgoUrl('zh', 'docs/'), 'https://nsgo.dev/zh/docs/')
    assert.equal(nsgoUrl('ja', 'docs/'), 'https://nsgo.dev/docs/')
    assert.equal(nsgoUrl('zh-tw'), 'https://nsgo.dev/')
  })
})

describe('copy and showcase completeness', () => {
  test('every language provides complete UI copy and formatters', () => {
    for (const lang of LANGS) {
      const copy = copies[lang]
      assert.ok(copy, `missing copy for ${lang}`)
      assert.ok(copy.site.title.length > 0, `${lang} site.title`)
      assert.ok(copy.site.description.length > 0, `${lang} site.description`)
      assert.equal(copy.home.title.length, 2, `${lang} home.title`)
      assert.ok(copy.home.lead.length > 0, `${lang} home.lead`)
      assert.equal(copy.home.stages.length, 2, `${lang} home.stages`)
      assert.equal(copy.home.how.length, 3, `${lang} home.how`)
      assert.equal(copy.home.workflow.length, 3, `${lang} home.workflow`)
      assert.equal(copy.home.readouts.length, 3, `${lang} home.readouts`)
      assert.equal(copy.home.steps.length, 3, `${lang} home.steps`)
      assert.equal(copy.categories.length, 6, `${lang} categories`)
      assert.equal(copy.footer.columns.length, 3, `${lang} footer.columns`)
      assert.ok(copy.home.packagesTitle.includes('{n}'), `${lang} home.packagesTitle`)
      assert.ok(copy.packages.title.includes('{n}'), `${lang} packages.title`)
      assert.ok(copy.home.finalShowcase.length > 0, `${lang} home.finalShowcase`)
      assert.ok(copy.footer.editor.length > 0, `${lang} footer.editor`)
      assert.ok(copy.footer.templates.length > 0, `${lang} footer.templates`)
      assert.ok(copy.docs.fallback.length > 0, `${lang} docs.fallback`)
      if (lang !== 'en') {
        assert.ok(copy.api.notice.length > 0, `${lang} api.notice`)
      }
      assert.ok(copy.playground.files(1).length > 0)
      assert.ok(copy.playground.files(5).length > 0)
      assert.ok(copy.playground.lines(1234).length > 0)
      assert.ok(copy.showcase.entry(1).length > 0)
      assert.ok(copy.playground.exampleTitle('Cave Run').includes('Cave Run'))
      assert.ok(copy.playground.running('Cave Run').includes('Cave Run'))
      assert.ok(copy.playground.download('2.4').includes('2.4'))
      assert.ok(copy.playground.playFullscreen.length > 0, `${lang} playground.playFullscreen`)
      assert.ok(copy.playground.exitFullscreen.length > 0, `${lang} playground.exitFullscreen`)
      assert.ok(copy.playground.touchHint.length > 0, `${lang} playground.touchHint`)
      assert.ok(copy.playground.rotateLandscape.length > 0, `${lang} playground.rotateLandscape`)
      assert.ok(copy.playground.rotatePortrait.length > 0, `${lang} playground.rotatePortrait`)
      assert.ok(copy.playground.continueAnyway.length > 0, `${lang} playground.continueAnyway`)
    }
  })

  test('Russian pluralization handles one, few, and many forms', () => {
    const ru = copies.ru
    assert.equal(ru.playground.files(1), '1 файл')
    assert.equal(ru.playground.files(2), '2 файла')
    assert.equal(ru.playground.files(5), '5 файлов')
    assert.equal(ru.playground.files(11), '11 файлов')
    assert.equal(ru.playground.files(21), '21 файл')
    assert.equal(ru.playground.files(24), '24 файла')
  })

  test('every showcase entry has titles and descriptions in all languages', () => {
    for (const entry of SHOWCASE_ENTRIES) {
      for (const lang of LANGS) {
        assert.ok(entry.title[lang].length > 0, `${entry.id} title.${lang}`)
        assert.ok(entry.description[lang].length > 0, `${entry.id} description.${lang}`)
      }
    }
  })
})

describe('localizeDocLinks', () => {
  test('rewrites internal docs links for fallback pages without touching external links or code blocks', () => {
    const html =
      '<p>See <a href="/docs/sprite/#atlases">sprites</a>, <a class="x" href="/docs/">index</a>, and <a href="https://nsgo.dev/docs/">NSGo</a>.</p><pre><code>&lt;a href="/docs/loop/"&gt;</code></pre>'
    assert.equal(localizeDocLinks(html, 'en', '/'), html)
    assert.equal(
      localizeDocLinks(html, 'ja', '/'),
      '<p>See <a href="/ja/docs/sprite/#atlases">sprites</a>, <a class="x" href="/ja/docs/">index</a>, and <a href="https://nsgo.dev/docs/">NSGo</a>.</p><pre><code>&lt;a href="/docs/loop/"&gt;</code></pre>',
    )
    const subBase = '<p><a href="/game/docs/loop/">loop</a></p>'
    assert.equal(localizeDocLinks(subBase, 'zh-tw', '/game/'), '<p><a href="/game/zh-tw/docs/loop/">loop</a></p>')
  })
})
