import React from 'react'
import { WIDGETS } from '../widgets'

// WidgetFrame draws the titled panel around a widget and routes to the right
// component. In edit mode the header doubles as the drag handle and shows a
// remove button. The title is editable inline.
export default function WidgetFrame({ widget, editMode, onChange, onRemove }) {
  const def = WIDGETS[widget.type]
  if (!def) return <div className="widget"><div className="center-note">Unknown widget: {widget.type}</div></div>
  const Component = def.component

  return (
    <div className="widget">
      <div className="widget-head drag-handle">
        <span className="title">
          {def.emoji}{' '}
          {editMode ? (
            <input
              value={widget.title}
              onChange={(e) => onChange({ ...widget, title: e.target.value })}
              onMouseDown={(e) => e.stopPropagation()} // don't start a drag when editing
              style={{ width: 120, padding: '2px 6px', fontSize: 13 }}
            />
          ) : (
            widget.title
          )}
        </span>
        {editMode && (
          <button className="head-btn" title="Remove widget" onMouseDown={(e) => e.stopPropagation()} onClick={onRemove}>🗑</button>
        )}
      </div>
      <div className="widget-body">
        <Component widget={widget} onChange={onChange} />
      </div>
    </div>
  )
}
