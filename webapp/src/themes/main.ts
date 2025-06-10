import { defineStyle, defineStyleConfig, extendTheme } from '@chakra-ui/react'

export const theme = extendTheme({
  colors: {
    purple: {
      500: '#855DCD',
    },
  },
  fonts: {
    heading: '"Inter", sans-serif',
    body: '"Inter", sans-serif',
  },
  styles: {
    global: {
      body: {
        bg: 'gray.50',
      },
    },
  },
  components: {
    Button: defineStyleConfig({
      defaultProps: {
        colorScheme: 'purple',
      },
    }),
    FormLabel: defineStyleConfig({
      baseStyle: {
        fontWeight: 500,
      },
    }),
    Heading: defineStyleConfig({
      baseStyle: {
        fontWeight: 500,
        color: 'gray.700',
      },
      sizes: {
        jumbo: defineStyle({
          fontSize: { base: '3xl', md: '4xl', xl: '5xl' },
          lineHeight: {
            base: 1.33,
            md: 1.2,
            xl: 1,
          },
        }),
      },
    }),
    Link: defineStyleConfig({
      baseStyle: {
        _hover: {
          color: 'purple.500',
          textDecoration: 'none',
        },
      },
      variants: {
        primary: {
          color: 'purple.500',
          _hover: {
            textDecoration: 'underline',
          },
        },
      },
    }),
  },
})
