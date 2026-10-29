import { createFileRoute, redirect } from '@tanstack/react-router'

import { ResourcePools } from '@/features/resource-pools'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/resource-pools/')({
  beforeLoad: () => {
    if ((useAuthStore.getState().auth.user?.role ?? 0) < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: ResourcePools,
})
