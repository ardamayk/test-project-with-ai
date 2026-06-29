import { createFileRoute } from '@tanstack/react-router'
import { ComingSoonPage } from '#/components/coming-soon-page'

export const Route = createFileRoute('/library/genres/')({
  component: GenresPage,
})

function GenresPage() {
  return <ComingSoonPage title="Genres" />
}
