"""Точка входа: python run.py [magnet|файл.torrent ...]"""

import sys

from tarbit.app import main

if __name__ == "__main__":
    sys.exit(main())
