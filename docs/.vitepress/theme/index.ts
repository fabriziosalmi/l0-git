import DefaultTheme from 'vitepress/theme'
import GateMeta from './components/GateMeta.vue'
import './custom.css'

export default {
  ...DefaultTheme,
  enhanceApp({ app }) {
    app.component('GateMeta', GateMeta)
  }
}
