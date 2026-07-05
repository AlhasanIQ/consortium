import type React from 'react';
import { useState } from 'react';

interface SchemaField {
  type?: string;
  label?: string;
  description?: string;
  required?: boolean;
  placeholder?: string;
}

interface InputDialogProps {
  isOpen: boolean;
  schema: Record<string, SchemaField>;
  onSubmit: (values: Record<string, unknown>) => void;
  onCancel: () => void;
}

export function InputDialog({ isOpen, schema, onSubmit, onCancel }: InputDialogProps) {
  const [values, setValues] = useState<Record<string, unknown>>({});

  if (!isOpen) return null;

  const schemaFields = Object.entries(schema);

  if (schemaFields.length === 0) {
    // No inputs required, execute immediately
    onSubmit({});
    return null;
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(values);
  };

  const handleChange = (key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  };

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(0, 0, 0, 0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 99999,
      }}
    >
      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.3), 0 10px 10px -5px rgba(0, 0, 0, 0.2)',
          padding: '24px',
          width: '100%',
          maxWidth: '28rem',
          maxHeight: '90vh',
          overflowY: 'auto',
        }}
      >
        <h2 style={{ fontSize: '20px', fontWeight: 600, marginBottom: '16px' }}>Workflow Inputs</h2>
        <p style={{ fontSize: '14px', color: '#666', marginBottom: '16px' }}>
          Please provide the following inputs to execute the workflow:
        </p>

        <form onSubmit={handleSubmit}>
          <div style={{ marginBottom: '24px' }}>
            {schemaFields.map(([key, fieldSchema]) => {
              const type = fieldSchema.type || 'text';
              const label = fieldSchema.label || key;
              const description = fieldSchema.description;
              const required = fieldSchema.required !== false;

              const inputStyle = {
                width: '100%',
                padding: '8px 12px',
                border: '1px solid #d1d5db',
                borderRadius: '6px',
                fontSize: '14px',
                outline: 'none',
              };

              return (
                <div key={key} style={{ marginBottom: '16px' }}>
                  <label
                    htmlFor={`input-${key}`}
                    style={{
                      display: 'block',
                      fontSize: '14px',
                      fontWeight: 500,
                      color: '#374151',
                      marginBottom: '4px',
                    }}
                  >
                    {label}
                    {required && <span style={{ color: '#ef4444', marginLeft: '4px' }}>*</span>}
                  </label>

                  {type === 'textarea' ? (
                    <textarea
                      id={`input-${key}`}
                      style={{ ...inputStyle, minHeight: '100px', resize: 'vertical' }}
                      value={(values[key] as string) || ''}
                      onChange={(e) => handleChange(key, e.target.value)}
                      required={required}
                      rows={4}
                      placeholder={fieldSchema.placeholder}
                    />
                  ) : type === 'number' ? (
                    <input
                      id={`input-${key}`}
                      type="number"
                      style={inputStyle}
                      value={(values[key] as number) || ''}
                      onChange={(e) => handleChange(key, parseFloat(e.target.value))}
                      required={required}
                      placeholder={fieldSchema.placeholder}
                    />
                  ) : (
                    <input
                      id={`input-${key}`}
                      type="text"
                      style={inputStyle}
                      value={(values[key] as string) || ''}
                      onChange={(e) => handleChange(key, e.target.value)}
                      required={required}
                      placeholder={fieldSchema.placeholder}
                    />
                  )}

                  {description && <p style={{ fontSize: '12px', color: '#6b7280', marginTop: '4px' }}>{description}</p>}
                </div>
              );
            })}
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
            <button
              type="button"
              onClick={onCancel}
              style={{
                padding: '8px 16px',
                fontSize: '14px',
                fontWeight: 500,
                color: '#374151',
                backgroundColor: '#f3f4f6',
                border: 'none',
                borderRadius: '6px',
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
            <button
              type="submit"
              style={{
                padding: '8px 16px',
                fontSize: '14px',
                fontWeight: 500,
                color: 'white',
                backgroundColor: '#2563eb',
                border: 'none',
                borderRadius: '6px',
                cursor: 'pointer',
              }}
            >
              Execute
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
