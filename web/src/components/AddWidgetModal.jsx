import React from 'react'
import { WIDGETS } from '../widgets'

// AddWidgetModal: pick a widget type to add via a button (no config-file
// editing). The actual add logic lives in App.
export default function AddWidgetModal({ onPick, onClose }) {
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>Add a widget</h2>
        <div className="widget-picker">
          {Object.entries(WIDGETS).map(([type, def]) => (
            <div key={type} className="widget-pick" onClick={() => onPick(type)}>
              <span className="emoji">{def.emoji}</span>
              {def.label}
            </div>
          ))}
        </div>
        <div style={{ marginTop: 16, textAlign: 'right' }}>
          <button className="btn" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  )
}
