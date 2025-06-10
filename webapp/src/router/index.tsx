import { lazy, Suspense } from 'react'
import { createHashRouter, RouterProvider } from 'react-router-dom'
import { Layout } from '~components/Layout'

const Dashboard = lazy(() => import('~pages/Dashboard'))

const SuspenseLoader = ({ children }: { children: React.ReactNode }) => (
  <Suspense fallback={<div>Loading...</div>}>{children}</Suspense>
)

export const Router = () => {
  const router = createHashRouter([
    {
      path: '/',
      element: <Layout />,
      children: [
        {
          path: '/',
          element: (
            <SuspenseLoader>
              <Dashboard />
            </SuspenseLoader>
          ),
        },
      ],
    },
  ])

  return <RouterProvider router={router} />
}
