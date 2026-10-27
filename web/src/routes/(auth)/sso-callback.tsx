import { createFileRoute, useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'

import { OAuthCallbackScreen } from '@/features/auth/components/oauth-callback-screen'
import { resolveAuthentication } from '@/lib/auth-session'

export const Route = createFileRoute('/(auth)/sso-callback')({
  validateSearch: (search: Record<string, unknown>) => ({
    access_token: (search.access_token as string) ?? '',
    redirect: (search.redirect as string) ?? '/model-square',
  }),
  component: SsoCallback,
})

function SsoCallback() {
  const { redirect } = useSearch({ from: '/(auth)/sso-callback' })
  const navigate = useNavigate()
  const handled = useRef(false)

  useEffect(() => {
    if (handled.current) return
    handled.current = true

    resolveAuthentication()
      .then(() => {
        navigate({ to: redirect || '/model-square', replace: true })
      })
      .catch(() => {
        navigate({ to: '/sign-in', replace: true })
      })
  }, [navigate, redirect])

  return <OAuthCallbackScreen provider='sso' mode='login' />
}
