import { useEffect } from "react";
import {
  BUILD_CHANNEL_CONFIG,
  decorateWindowTitle,
} from "../../../shared/build-channel";

/** Sets document.title. The tab system observes this automatically. */
export function useDocumentTitle(title: string) {
  useEffect(() => {
    if (title) {
      document.title = decorateWindowTitle(
        title,
        BUILD_CHANNEL_CONFIG.titlePrefix,
        BUILD_CHANNEL_CONFIG.titleFallback,
      );
    }
  }, [title]);
}
