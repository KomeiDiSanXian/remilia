import { Component, type ReactNode, type ErrorInfo } from 'react'
import { TitleBar } from './TitleBar.tsx'
import { invoke } from '@tauri-apps/api/core'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleQuit = () => {
    invoke('quit_app').catch(() => {})
  }

  render() {
    if (this.state.error) {
      return (
        <div className="app">
          <TitleBar />
          <div className="error-boundary">
            <h1>应用出错了</h1>
            <p className="error-msg">{this.state.error.message}</p>
            <pre className="error-stack">{this.state.error.stack}</pre>
            <div className="error-actions">
              <button onClick={() => this.setState({ error: null })}>重试</button>
              <button className="warn" onClick={this.handleQuit}>退出应用</button>
            </div>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
