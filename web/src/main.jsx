import React from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import ArticleReader from './components/ArticleReader'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import './styles.css'

createRoot(document.getElementById('root')).render(window.location.pathname === '/read' ? <ArticleReader /> : <App />)
