import { useMDXComponents as getNextraComponents } from 'nextra-theme-docs'
import { CurrentVersion } from './components/current-version'
import { CTARow, FeatureGrid, Hero, HeroImage } from './components/landing'

const sharedComponents = {
  CurrentVersion,
  Hero,
  CTARow,
  FeatureGrid,
  HeroImage,
}

export function useMDXComponents(components?: Record<string, unknown>) {
  return {
    ...getNextraComponents(),
    ...sharedComponents,
    ...components,
  }
}
