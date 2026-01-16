# timescrapercontroller

Time-based implementation of `extension/xextension/scrapercontroller.ScraperController`.

## Extension type

- `timer_controller`

## Configuration

- `collection_interval` (required): how often to trigger scraping
- `initial_delay` (optional): delay before the first trigger
- `timeout` (optional): context deadline used for both scrape and consume

## Behavior

On each tick, the controller:

1. calls `ScrapeMetrics` / `ScrapeLogs` on each registered scraper
2. forwards the result to the registered consumer via `ConsumeMetrics` / `ConsumeLogs`

