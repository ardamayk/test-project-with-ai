import { createFileRoute } from '@tanstack/react-router'
import { ComingSoonPage } from '#/components/coming-soon-page'
import { MainHeader } from '#/components/main-header'

export const Route = createFileRoute('/library/genres/')({
  component: GenresPage,
})

function GenresPage() {
  return (
    <>
      <MainHeader />
      <ComingSoonPage title="Genres" />
    </>
  )
}
