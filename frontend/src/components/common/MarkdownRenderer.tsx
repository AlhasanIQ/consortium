import DOMPurify from 'dompurify';
import { marked } from 'marked';
import type React from 'react';
import { useMemo } from 'react';

interface MarkdownRendererProps {
  content: string;
  style?: React.CSSProperties;
}

/**
 * Lightweight markdown renderer using marked library.
 * Renders markdown content as sanitized HTML.
 */
const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({ content, style }) => {
  const htmlContent = useMemo(() => {
    try {
      // Configure marked for better rendering
      marked.setOptions({
        breaks: true, // Convert \n to <br>
        gfm: true, // GitHub Flavored Markdown
      });

      const parsed = marked.parse(content) as string;
      return DOMPurify.sanitize(parsed, { USE_PROFILES: { html: true } });
    } catch (error) {
      console.error('Error parsing markdown:', error);
      return content; // Fallback to plain text
    }
  }, [content]);

  return (
    <div
      style={{
        ...style,
        lineHeight: '1.6',
      }}
      // biome-ignore lint/security/noDangerouslySetInnerHtml: Required for markdown rendering
      dangerouslySetInnerHTML={{ __html: htmlContent }}
      className="markdown-content"
    />
  );
};

export default MarkdownRenderer;
