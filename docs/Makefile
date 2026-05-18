.PHONY: default dev build preview install clean

default: dev

install:
	npm install

dev: install
	npm run dev

build: install
	npm run build

preview: build
	npm run preview

clean:
	rm -rf .vitepress/dist .vitepress/cache
