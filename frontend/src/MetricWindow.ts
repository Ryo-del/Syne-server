import { WebviewWindow } from '@tauri-apps/api/webviewWindow'

export type Metric =
  | 'CPU'
  | 'RAM'
  | 'Network'
  | 'Online Users'
  | 'Connections'
  | 'Study'
  | 'Teacher'
  | 'Errors'

const metricWindows: Record<
  Metric,
  {
    label: string
    path: string
    title: string
  }
> = {
  CPU: {
    label: 'metric-cpu',
    path: '/metric/cpu',
    title: 'CPU',
  },

  RAM: {
    label: 'metric-ram',
    path: '/metric/ram',
    title: 'RAM',
  },

  Network: {
    label: 'metric-network',
    path: '/metric/network',
    title: 'Network',
  },

  'Online Users': {
    label: 'metric-online-users',
    path: '/metric/online-users',
    title: 'Online Users',
  },

  Connections: {
    label: 'metric-connections',
    path: '/metric/connections',
    title: 'Connections',
  },

  Study: {
    label: 'metric-study',
    path: '/metric/study',
    title: 'Study',
  },

  Teacher: {
    label: 'metric-teacher',
    path: '/metric/teacher',
    title: 'Teacher',
  },

  Errors: {
    label: 'metric-errors',
    path: '/metric/errors',
    title: 'Errors',
  },
}

export async function openMetric(metric: Metric) {
  const config = metricWindows[metric]

  const existingWindow =
    await WebviewWindow.getByLabel(config.label)

  if (existingWindow) {
    await existingWindow.show()
    await existingWindow.setFocus()
    return
  }

  const metricWindow = new WebviewWindow(config.label, {
    url: config.path,
    title: config.title,

    width: 720,
    height: 450,

    minWidth: 500,
    minHeight: 350,

    resizable: true,
    center: true,
    focus: true,
  })

  metricWindow.once('tauri://created', () => {
    console.log(`Metric window created: ${metric}`)
  })

  metricWindow.once('tauri://error', (error) => {
    console.error(`Failed to create ${metric} window:`, error)
  })
}