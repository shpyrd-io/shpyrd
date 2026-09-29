import { Fragment } from 'react'
import Image from 'next/image'
import clsx from 'clsx'
import Highlight, { defaultProps } from 'prism-react-renderer'

import { Button } from '@/components/Button'
import { HeroBackground } from '@/components/HeroBackground'
import blurCyanImage from '@/images/blur-cyan.png'
import blurIndigoImage from '@/images/blur-indigo.png'
import shpyrdBackground from '@/images/shpyrd-background.png'

const codeLanguage = 'bash'
const code = `$ shpyrd projects create hello-world
$ shpyrd deploy
==> Archiving HEAD (654f4925638e)
==> Building
===> detect  4 of 9 buildpacks participating
===> build   web, worker
==> Releasing
    Running: web 3/3 · worker 2/2
Released v1: Initial deploy 654f4925638e
https://hello-world.127.0.0.1.nip.io`

const tabs = [
  { name: 'terminal', isActive: true },
  { name: 'shpyrd.yaml', isActive: false },
]

function TrafficLightsIcon(props) {
  return (
    <svg aria-hidden="true" viewBox="0 0 42 10" fill="none" {...props}>
      <circle cx="5" cy="5" r="4.5" />
      <circle cx="21" cy="5" r="4.5" />
      <circle cx="37" cy="5" r="4.5" />
    </svg>
  )
}

export function Hero() {
  return (
    <div className="dark:-mb-32 dark:mt-[-4.75rem] dark:pb-32 dark:pt-[4.75rem]">
      <div className="py-16 sm:px-2 lg:relative lg:px-0 lg:py-20">
        <div className="mx-auto grid max-w-2xl grid-cols-1 items-center gap-x-8 gap-y-16 px-4 lg:max-w-8xl lg:grid-cols-2 lg:px-8 xl:gap-x-16 xl:px-12">
          <div className="relative z-10 md:text-center lg:text-left">
            <Image
              className="absolute bottom-full right-full -mb-56 -mr-72 opacity-50"
              src={blurCyanImage}
              alt=""
              width={530}
              height={530}
              unoptimized
              priority
            />
            <div className="relative">
              <p className="inline bg-gradient-to-r from-orange-600 via-orange-400 to-orange-600 bg-clip-text font-display text-5xl tracking-tight text-transparent">
                Opensource Cloud PaaS
              </p>
              <p className="mt-3 text-2xl tracking-tight text-slate-400">
                Manage applications and agents stack from one place, from deploy to monitoring.
              </p>
              <div className="mt-8 flex gap-4 md:justify-center lg:justify-start">
                <Button href="/#quick-start">Get started</Button>
                <Button href="https://github.com/shpyrd-io/shpyrd" variant="secondary">
                  View on GitHub
                </Button>
              </div>
            </div>
          </div>
          <div className="relative hidden lg:block lg:static xl:pl-10">
            <div className="relative">
              <Image
                className="absolute -right-20 -top-64"
                src={shpyrdBackground}
                alt=""
                width={530}
                height={530}
                unoptimized
                priority
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
