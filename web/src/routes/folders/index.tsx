import { createFileRoute } from '@tanstack/react-router'
import { ComingSoonPage } from '#/components/coming-soon-page'

export const Route = createFileRoute('/folders/')({
  component: () => <ComingSoonPage title="Folders" />,
})
