export class ToastNotifier {
  private client: any
  private directory: string

  constructor(client: any, directory: string) {
    this.client = client
    this.directory = directory
  }

  private async showToast(
    message: string,
    variant: 'info' | 'success' | 'warning' | 'error',
    title?: string,
    duration?: number
  ): Promise<void> {
    try {
      if (this.client?.tui?.showToast) {
        await this.client.tui.showToast({
          body: { title, message, variant, duration: duration || 3000 },
          query: { directory: this.directory }
        })
      }
    } catch {
      // Silent fail - toast is non-critical
    }
  }

  async success(message: string, title?: string, duration?: number): Promise<void> {
    await this.showToast(message, 'success', title, duration || 3000)
  }

  async error(message: string, title?: string, duration?: number): Promise<void> {
    await this.showToast(message, 'error', title, duration || 5000)
  }

  async warning(message: string, title?: string, duration?: number): Promise<void> {
    await this.showToast(message, 'warning', title, duration || 4000)
  }

  async info(message: string, title?: string, duration?: number): Promise<void> {
    await this.showToast(message, 'info', title, duration || 3000)
  }

  async progress(message: string, title?: string, progress?: number): Promise<void> {
    const msg = progress !== undefined ? `${message} (${progress}%)` : message
    const dur = progress !== undefined ? 0 : 2000
    await this.showToast(msg, 'info', title, dur)
  }

  async detailed(options: {
    title?: string
    message: string
    variant?: 'info' | 'success' | 'warning' | 'error'
    duration?: number
  }): Promise<void> {
    await this.showToast(
      options.message,
      options.variant || 'info',
      options.title,
      options.duration
    )
  }
}
