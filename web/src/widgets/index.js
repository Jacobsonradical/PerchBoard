// Central registry of widget types: their component, picker label, and emoji.
// Adding a new widget type = one entry here + its component file.
import ClockWidget from './ClockWidget'
import WeatherWidget from './WeatherWidget'
import RSSWidget from './RSSWidget'
import HackerNewsWidget from './HackerNewsWidget'
import MarketsWidget from './MarketsWidget'
import CalendarWidget from './CalendarWidget'
import ScholarOneWidget from './ScholarOneWidget'

export const WIDGETS = {
  clock: { component: ClockWidget, label: 'Clock', emoji: '🕐' },
  weather: { component: WeatherWidget, label: 'Weather', emoji: '⛅' },
  rss: { component: RSSWidget, label: 'RSS Feed', emoji: '📰' },
  hackernews: { component: HackerNewsWidget, label: 'Hacker News', emoji: '🟧' },
  markets: { component: MarketsWidget, label: 'Markets', emoji: '📈' },
  calendar: { component: CalendarWidget, label: 'Calendar', emoji: '📅' },
  scholarone: { component: ScholarOneWidget, label: 'ScholarOne', emoji: '🎓' },
}
