import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { Dashboard } from '..'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/lib/api', () => ({ api: { get } }))
// Mock canvas rendering; keep dashboard components real.
vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

const clients: QueryClient[] = []

async function renderDashboard(role: number, section = 'models') {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'alice', role })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute({ component: Outlet })
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
    component: Outlet,
  })
  const dashboard = createRoute({
    getParentRoute: () => authenticated,
    path: 'dashboard/$section',
    component: Dashboard,
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([dashboard])]),
    history: createMemoryHistory({ initialEntries: [`/dashboard/${section}`] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  localStorage.clear()
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  // jsdom does not implement the Web Animations API used by ScrollArea.
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  get.mockImplementation((url: string) => {
    if (url.startsWith('/api/data/iself')) {
      return Promise.resolve({ data: { success: true, data: [] } })
    }
    if (url.startsWith('/api/data/analytics')) {
      return Promise.resolve({
        data: { success: true, data: { summary: null, rows: [] } },
      })
    }
    return new Promise(() => undefined)
  })
})

afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
  Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

test('users can open an empty iSelf table with the button and hide it again', async () => {
  const user = userEvent.setup()
  await renderDashboard(ROLE.USER)
  const button = await screen.findByRole('button', { name: 'iSelf data' })
  expect(button).toHaveAttribute('aria-pressed', 'false')
  expect(screen.queryByRole('region', { name: 'iSelf data' })).toBeNull()
  expect(
    get.mock.calls.filter(([url]) => String(url).startsWith('/api/data/iself'))
  ).toHaveLength(0)

  await user.click(button)

  expect(
    await screen.findByRole('region', { name: 'iSelf data' })
  ).toBeInTheDocument()
  expect(
    await screen.findByText('No iSelf usage in this time range')
  ).toBeInTheDocument()
  expect(button).toHaveAttribute('aria-pressed', 'true')
  expect(get).toHaveBeenCalledWith('/api/data/iself/self', expect.anything())
  const modelMetrics = await screen.findByRole('heading', {
    name: 'Model metrics',
  })
  const iselfTable = screen.getByRole('region', { name: 'iSelf data' })
  expect(modelMetrics.compareDocumentPosition(iselfTable)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING
  )
  expect(
    iselfTable.compareDocumentPosition(
      screen.getByRole('heading', { name: 'Model availability' })
    )
  ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)

  await user.click(button)
  expect(screen.queryByRole('region', { name: 'iSelf data' })).toBeNull()
  expect(button).toHaveAttribute('aria-pressed', 'false')
})

test('administrators can open the iSelf table from user analytics', async () => {
  const user = userEvent.setup()
  await renderDashboard(ROLE.ADMIN, 'users')
  await user.click(await screen.findByRole('button', { name: 'iSelf data' }))
  expect(
    await screen.findByText('No iSelf usage in this time range')
  ).toBeInTheDocument()
  const userUsage = await screen.findByRole('heading', {
    name: 'User usage details',
  })
  const iselfTable = screen.getByRole('region', { name: 'iSelf data' })
  expect(userUsage.compareDocumentPosition(iselfTable)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING
  )
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/data/iself', expect.anything())
  )
})

test('applying a new dashboard time range reloads the visible iSelf table', async () => {
  const user = userEvent.setup()
  await renderDashboard(ROLE.USER)
  await user.click(await screen.findByRole('button', { name: 'iSelf data' }))
  await screen.findByText('No iSelf usage in this time range')

  await user.click(screen.getByRole('button', { name: 'Filter' }))
  await user.click(await screen.findByRole('button', { name: '7 Days' }))
  await user.click(screen.getByRole('button', { name: 'Apply Filters' }))

  await waitFor(() => {
    const latest = [...get.mock.calls]
      .reverse()
      .find(([url]) => url === '/api/data/iself/self')
    expect(
      latest?.[1]?.params.end_timestamp - latest?.[1]?.params.start_timestamp
    ).toBe(7 * 86400)
  })
})
