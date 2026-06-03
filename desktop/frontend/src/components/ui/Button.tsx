import React from 'react'

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary'
  size?: 'sm' | 'md'
}

const BASE =
  'inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card disabled:opacity-50 disabled:cursor-not-allowed'

const SIZE: Record<NonNullable<ButtonProps['size']>, string> = {
  sm: 'text-[13px] px-3 h-8',
  md: 'text-[14px] px-4 h-9',
}

const VARIANT: Record<NonNullable<ButtonProps['variant']>, string> = {
  primary: 'bg-accent text-[#0b0f14] hover:bg-[#5aa6f0] active:bg-[#4f97e0]',
  secondary:
    'bg-card border border-border text-secondary hover:bg-raised hover:border-accent/60 active:bg-raised',
}

export function Button({
  variant = 'secondary',
  size = 'sm',
  type = 'button',
  className = '',
  children,
  ...rest
}: ButtonProps) {
  const cls = `${BASE} ${SIZE[size]} ${VARIANT[variant]} ${className}`.trim()
  return (
    <button type={type} className={cls} {...rest}>
      {children}
    </button>
  )
}
