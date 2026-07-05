import React from 'react'
import GridLayout, { WidthProvider } from 'react-grid-layout'
import WidgetFrame from './WidgetFrame'

// WidthProvider makes the grid fill its container width responsively.
const Grid = WidthProvider(GridLayout)

// Dashboard renders the draggable/resizable grid of widgets.
// In edit mode the user drags widgets by their header and resizes from the
// corner; layout changes are reported up and persisted. Outside edit mode the
// grid is locked so normal clicks (links, buttons) work cleanly.
export default function Dashboard({ config, editMode, onLayoutChange, onWidgetChange, onWidgetRemove }) {
  return (
    <div className={'dashboard' + (editMode ? ' edit-mode' : '')}>
      <Grid
        className="layout"
        layout={config.layout}
        cols={12}
        rowHeight={56}
        margin={[12, 12]}
        isDraggable={editMode}
        isResizable={editMode}
        draggableHandle=".drag-handle"
        onLayoutChange={(l) => editMode && onLayoutChange(l)}
        compactType="vertical"
      >
        {config.widgets.map((w) => (
          <div key={w.id}>
            <WidgetFrame
              widget={w}
              editMode={editMode}
              onChange={onWidgetChange}
              onRemove={() => onWidgetRemove(w.id)}
            />
          </div>
        ))}
      </Grid>
    </div>
  )
}
