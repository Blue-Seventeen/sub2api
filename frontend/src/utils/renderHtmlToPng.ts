import html2canvas from 'html2canvas'

export interface RenderElementToPngOptions {
  elementId: string
  width?: number
  height?: number
  scaleMultiplier?: number
  pixelRatio?: number
  backgroundColor?: string | null
}

function createSandboxWrapper(width?: number, height?: number) {
  const wrapper = document.createElement('div')
  wrapper.style.position = 'fixed'
  wrapper.style.left = '-20000px'
  wrapper.style.top = '0'
  wrapper.style.pointerEvents = 'none'
  wrapper.style.opacity = '1'
  wrapper.style.zIndex = '-1'
  wrapper.style.background = 'transparent'
  wrapper.style.margin = '0'
  wrapper.style.padding = '0'
  wrapper.style.overflow = 'hidden'
  if (width) {
    wrapper.style.width = `${width}px`
  }
  if (height) {
    wrapper.style.height = `${height}px`
  }
  wrapper.setAttribute('aria-hidden', 'true')
  return wrapper
}

function getElementSize(element: HTMLElement) {
  const rect = element.getBoundingClientRect()
  const computed = window.getComputedStyle(element)

  return {
    width: Math.ceil(
      rect.width ||
        Number.parseFloat(computed.width) ||
        element.scrollWidth ||
        element.offsetWidth ||
        0
    ),
    height: Math.ceil(
      rect.height ||
        Number.parseFloat(computed.height) ||
        element.scrollHeight ||
        element.offsetHeight ||
        0
    )
  }
}

function prepareClonedTree(clonedRoot: HTMLElement) {
  clonedRoot.removeAttribute('id')
  clonedRoot.style.margin = '0'
  clonedRoot.style.transform = 'none'
  clonedRoot.style.transformOrigin = 'top left'
  clonedRoot.style.position = 'relative'
  clonedRoot.style.left = '0'
  clonedRoot.style.top = '0'
  clonedRoot.style.animation = 'none'

  const descendants = clonedRoot.querySelectorAll<HTMLElement>('*')
  descendants.forEach((node) => {
    node.style.animation = 'none'
    node.style.transition = 'none'
  })

  const titleNodes = clonedRoot.querySelectorAll<HTMLElement>('[data-poster-role="title"]')
  titleNodes.forEach((node) => {
    node.style.background = 'none'
    node.style.color = '#dffafe'
    node.style.setProperty('-webkit-text-fill-color', '#dffafe')
    node.style.setProperty('-webkit-background-clip', 'border-box')
    node.style.textShadow = 'none'
  })

  const chipNodes = clonedRoot.querySelectorAll<HTMLElement>('[data-poster-role="chip"]')
  chipNodes.forEach((node) => {
    node.style.height = '28px'
    node.style.padding = '0 12px'
    node.style.lineHeight = '1'
    node.style.boxSizing = 'border-box'
    node.style.whiteSpace = 'nowrap'
    node.style.flex = '0 0 auto'
  })
}

async function waitForImages(container: HTMLElement) {
  const images = Array.from(container.querySelectorAll('img'))
  await Promise.all(
    images.map((img) => {
      if (img.complete && img.naturalWidth > 0) {
        return Promise.resolve()
      }
      return new Promise<void>((resolve) => {
        const done = () => resolve()
        img.addEventListener('load', done, { once: true })
        img.addEventListener('error', done, { once: true })
      })
    })
  )
}

function waitForNextPaint() {
  return new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  })
}

export async function renderElementToPngBlobById(options: RenderElementToPngOptions) {
  const source = document.getElementById(options.elementId)
  if (!(source instanceof HTMLElement)) {
    throw new Error(`Poster element not found: ${options.elementId}`)
  }

  const measured = getElementSize(source)
  const width = Math.max(1, options.width ?? measured.width)
  const height = Math.max(1, options.height ?? measured.height)
  const scaleMultiplier = Math.max(1, options.scaleMultiplier ?? 1)
  const pixelRatio = Math.max(1, options.pixelRatio ?? 2)

  const wrapper = createSandboxWrapper(width, height)
  const clonedRoot = source.cloneNode(true) as HTMLElement
  prepareClonedTree(clonedRoot)
  wrapper.appendChild(clonedRoot)
  document.body.appendChild(wrapper)

  try {
    if ('fonts' in document) {
      await document.fonts.ready
    }

    await waitForImages(wrapper)
    await waitForNextPaint()

    const canvas = await html2canvas(clonedRoot, {
      scale: pixelRatio * scaleMultiplier,
      useCORS: true,
      allowTaint: false,
      backgroundColor: options.backgroundColor ?? null,
      logging: false,
      imageTimeout: 0,
      width,
      height
    })

    return await new Promise<Blob | null>((resolve) => {
      canvas.toBlob(resolve, 'image/png', 1)
    })
  } finally {
    wrapper.remove()
  }
}
